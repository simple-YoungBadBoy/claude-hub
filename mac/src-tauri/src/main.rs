// Claude Hub for macOS: a thin native shell around the hub UI served by the
// server over Tailscale. All traffic (Claude, tmux, uploads) goes through the
// server; this app only renders the page and shows native notifications.
//
// The webview never talks to the server directly: system proxies such as
// Clash send WebKit traffic for 100.x / *.ts.net out to the internet, which
// breaks it. Instead the app forwards 127.0.0.1:17788 (always exempt from
// system proxies) to the server over a plain TCP connection.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::fs;
use std::io;
use std::net::{Shutdown, TcpListener, TcpStream, ToSocketAddrs};
use std::path::PathBuf;
use std::sync::Mutex;
use std::thread;
use std::time::Duration;

use tauri::menu::{MenuBuilder, MenuItemBuilder, SubmenuBuilder};
use tauri::{AppHandle, Manager, Url};

const LOCAL_PORT: u16 = 17788;

/// host:port the local forwarder connects to.
static TARGET: Mutex<Option<String>> = Mutex::new(None);
/// Port the forwarder actually listens on.
static LOCAL: Mutex<Option<u16>> = Mutex::new(None);

fn config_file(app: &AppHandle) -> Option<PathBuf> {
    let dir = app.path().app_config_dir().ok()?;
    let _ = fs::create_dir_all(&dir);
    Some(dir.join("server.txt"))
}

fn saved_server(app: &AppHandle) -> Option<String> {
    let s = fs::read_to_string(config_file(app)?).ok()?;
    let s = s.trim().to_string();
    if s.starts_with("http://") {
        Some(s)
    } else {
        None
    }
}

fn host_port(url: &str) -> Result<String, String> {
    let u: Url = url.parse().map_err(|e| format!("地址无效: {e}"))?;
    if u.scheme() != "http" {
        return Err("地址要以 http:// 开头".into());
    }
    let host = u.host_str().ok_or("地址里没有主机名")?;
    Ok(format!("{host}:{}", u.port().unwrap_or(7788)))
}

fn pipe(mut from: TcpStream, mut to: TcpStream) {
    let _ = io::copy(&mut from, &mut to);
    let _ = to.shutdown(Shutdown::Write);
    let _ = from.shutdown(Shutdown::Read);
}

fn handle(client: TcpStream) {
    let Some(target) = TARGET.lock().unwrap().clone() else {
        return;
    };
    let upstream = target
        .to_socket_addrs()
        .ok()
        .and_then(|mut a| a.next())
        .and_then(|addr| TcpStream::connect_timeout(&addr, Duration::from_secs(8)).ok());
    let Some(upstream) = upstream else {
        let mut c = client;
        let body = format!(
            "<meta charset=utf-8><body style='background:#161412;color:#ece6de;font:14px -apple-system;padding:40px'>\
             <h3>连不上服务器 {target}</h3><p>检查 Mac 的 Tailscale 是否已连接、服务器上 claude-hub 是否在运行，然后按 ⌘R 重试。\
             按 ⌘, 可以修改服务器地址。</p>"
        );
        let _ = io::Write::write_all(
            &mut c,
            format!(
                "HTTP/1.1 502 Bad Gateway\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            )
            .as_bytes(),
        );
        return;
    };
    let _ = client.set_nodelay(true);
    let _ = upstream.set_nodelay(true);
    let (c2, u2) = match (client.try_clone(), upstream.try_clone()) {
        (Ok(c), Ok(u)) => (c, u),
        _ => return,
    };
    thread::spawn(move || pipe(c2, u2));
    pipe(upstream, client);
}

fn start_forwarder() -> Option<u16> {
    let listener = TcpListener::bind(("127.0.0.1", LOCAL_PORT))
        .or_else(|_| TcpListener::bind(("127.0.0.1", 0)))
        .ok()?;
    let port = listener.local_addr().ok()?.port();
    thread::spawn(move || {
        for conn in listener.incoming().flatten() {
            thread::spawn(move || handle(conn));
        }
    });
    Some(port)
}

fn local_url() -> Option<Url> {
    let port = (*LOCAL.lock().unwrap())?;
    format!("http://127.0.0.1:{port}/").parse().ok()
}

/// Saved server URL for the settings page.
#[tauri::command]
fn get_server(app: AppHandle) -> Option<String> {
    saved_server(&app)
}

/// Save the server URL, point the forwarder at it and load the hub.
#[tauri::command]
fn open_server(app: AppHandle, url: String) -> Result<(), String> {
    let url = url.trim().trim_end_matches('/').to_string();
    let target = host_port(&url)?;
    if let Some(f) = config_file(&app) {
        fs::write(f, &url).map_err(|e| e.to_string())?;
    }
    *TARGET.lock().unwrap() = Some(target);
    let w = app.get_webview_window("main").ok_or("没有主窗口")?;
    let local = local_url().ok_or("本地转发启动失败")?;
    w.navigate(local).map_err(|e| e.to_string())
}

fn main() {
    *LOCAL.lock().unwrap() = start_forwarder();

    tauri::Builder::default()
        .plugin(tauri_plugin_notification::init())
        .invoke_handler(tauri::generate_handler![get_server, open_server])
        .setup(|app| {
            if let Some(url) = saved_server(app.handle()) {
                if let Ok(t) = host_port(&url) {
                    *TARGET.lock().unwrap() = Some(t);
                    if let (Some(w), Some(u)) = (app.get_webview_window("main"), local_url()) {
                        let _ = w.navigate(u);
                    }
                }
            }

            let settings = MenuItemBuilder::with_id("settings", "更改服务器…")
                .accelerator("CmdOrCtrl+,")
                .build(app)?;
            let reload = MenuItemBuilder::with_id("reload", "重新加载")
                .accelerator("CmdOrCtrl+R")
                .build(app)?;
            let app_menu = SubmenuBuilder::new(app, "Claude Hub")
                .about(None)
                .separator()
                .item(&settings)
                .separator()
                .hide()
                .hide_others()
                .show_all()
                .separator()
                .quit()
                .build()?;
            // The Edit menu is what makes ⌘C / ⌘V / ⌘A work inside the webview.
            let edit = SubmenuBuilder::new(app, "编辑")
                .undo()
                .redo()
                .separator()
                .cut()
                .copy()
                .paste()
                .select_all()
                .build()?;
            let view = SubmenuBuilder::new(app, "显示")
                .item(&reload)
                .separator()
                .fullscreen()
                .build()?;
            let window = SubmenuBuilder::new(app, "窗口")
                .minimize()
                .maximize()
                .separator()
                .close_window()
                .build()?;
            let menu = MenuBuilder::new(app)
                .items(&[&app_menu, &edit, &view, &window])
                .build()?;
            app.set_menu(menu)?;

            app.on_menu_event(|app, event| {
                let Some(w) = app.get_webview_window("main") else {
                    return;
                };
                match event.id().as_ref() {
                    "settings" => {
                        if let Ok(url) = "tauri://localhost/index.html?settings=1".parse() {
                            let _ = w.navigate(url);
                        }
                    }
                    "reload" => {
                        let _ = w.eval("window.location.reload()");
                    }
                    _ => {}
                }
            });
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running Claude Hub");
}
