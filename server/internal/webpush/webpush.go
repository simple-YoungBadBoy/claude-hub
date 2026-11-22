// Package webpush sends Web Push notifications (RFC 8030) to the browsers
// that subscribed — on iPhone a home-screen web app — so "Claude finished /
// needs you" reaches the lock screen. Payloads are encrypted for the device
// (RFC 8291, aes128gcm): the push service (Apple, Google, Mozilla) relays
// them but can't read them. The server is authenticated with VAPID
// (RFC 8292). Standard library only.
package webpush

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var b64 = base64.RawURLEncoding

// Subscription is what PushManager.subscribe() returns in the browser.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Added time.Time `json:"added"`
}

// Push services of the browsers we support. Endpoints elsewhere are refused,
// so a stored subscription can't make the server call arbitrary URLs.
var allowedHosts = []string{"web.push.apple.com", "fcm.googleapis.com", "updates.push.services.mozilla.com", ".notify.windows.com"}

func checkEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return errors.New("推送地址无效")
	}
	h := strings.ToLower(u.Hostname())
	for _, a := range allowedHosts {
		if h == a || (strings.HasPrefix(a, ".") && strings.HasSuffix(h, a)) {
			return nil
		}
	}
	return fmt.Errorf("不支持的推送服务：%s", h)
}

// Pusher keeps the VAPID key and the subscriptions (files in the data dir).
type Pusher struct {
	mu      sync.Mutex
	key     *ecdsa.PrivateKey
	subs    []Subscription
	subPath string
	Subject string       // VAPID "sub": a contact URL or mailto:
	Client  *http.Client // nil = 10s timeout default
}

// Open loads (or creates) the VAPID key and the subscription list.
func Open(keyPath, subPath string) (*Pusher, error) {
	p := &Pusher{subPath: subPath, Subject: "https://github.com/simple-YoungBadBoy/claude-hub"}
	if b, err := os.ReadFile(keyPath); err == nil {
		blk, _ := pem.Decode(b)
		if blk == nil {
			return nil, errors.New("vapid key: bad PEM")
		}
		k, err := x509.ParseECPrivateKey(blk.Bytes)
		if err != nil {
			return nil, err
		}
		p.key = k
	} else {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		der, _ := x509.MarshalECPrivateKey(k)
		if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
			return nil, err
		}
		p.key = k
	}
	if b, err := os.ReadFile(subPath); err == nil {
		json.Unmarshal(b, &p.subs)
	}
	return p, nil
}

// PublicKey is the VAPID key for PushManager.subscribe (applicationServerKey).
func (p *Pusher) PublicKey() string {
	pub, _ := p.key.PublicKey.ECDH()
	return b64.EncodeToString(pub.Bytes())
}

// Count is how many devices are subscribed.
func (p *Pusher) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.subs)
}

// Has reports whether this endpoint is subscribed.
func (p *Pusher) Has(endpoint string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.subs {
		if s.Endpoint == endpoint {
			return true
		}
	}
	return false
}

func (p *Pusher) save() {
	b, _ := json.MarshalIndent(p.subs, "", "  ")
	tmp := p.subPath + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, p.subPath)
	}
}

// Add stores a subscription (replacing one with the same endpoint).
func (p *Pusher) Add(s Subscription) error {
	if err := checkEndpoint(s.Endpoint); err != nil {
		return err
	}
	if k, err := b64.DecodeString(strings.TrimRight(s.Keys.P256dh, "=")); err != nil || len(k) != 65 {
		return errors.New("p256dh 无效")
	}
	if a, err := b64.DecodeString(strings.TrimRight(s.Keys.Auth, "=")); err != nil || len(a) < 16 {
		return errors.New("auth 无效")
	}
	s.Added = time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.subs[:0]
	for _, x := range p.subs {
		if x.Endpoint != s.Endpoint {
			out = append(out, x)
		}
	}
	p.subs = append(out, s)
	if len(p.subs) > 20 { // keep the newest devices
		p.subs = p.subs[len(p.subs)-20:]
	}
	p.save()
	return nil
}

// Remove forgets a subscription.
func (p *Pusher) Remove(endpoint string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.subs[:0]
	for _, x := range p.subs {
		if x.Endpoint != endpoint {
			out = append(out, x)
		}
	}
	p.subs = out
	p.save()
}

// Message is the JSON the service worker turns into a notification.
type Message struct {
	Title   string `json:"title"`
	Body    string `json:"body"`
	Session string `json:"session,omitempty"` // opened on tap
	Tag     string `json:"tag,omitempty"`     // replaces an older notification with the same tag
}

// SendAll pushes m to every subscription; gone ones (404/410) are dropped.
func (p *Pusher) SendAll(m Message) {
	p.mu.Lock()
	subs := append([]Subscription(nil), p.subs...)
	p.mu.Unlock()
	body, _ := json.Marshal(m)
	for _, s := range subs {
		code, err := p.send(s, body)
		if code == http.StatusNotFound || code == http.StatusGone {
			p.Remove(s.Endpoint)
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "webpush %s: %v\n", hostOf(s.Endpoint), err)
		}
	}
}

func hostOf(raw string) string {
	u, _ := url.Parse(raw)
	if u == nil {
		return "?"
	}
	return u.Host
}

func (p *Pusher) send(s Subscription, payload []byte) (int, error) {
	if err := checkEndpoint(s.Endpoint); err != nil {
		return 0, err
	}
	body, err := Encrypt(s.Keys.P256dh, s.Keys.Auth, payload)
	if err != nil {
		return 0, err
	}
	jwt, err := p.vapidJWT(s.Endpoint)
	if err != nil {
		return 0, err
	}
	req, _ := http.NewRequest("POST", s.Endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", "3600")
	req.Header.Set("Urgency", "high")
	req.Header.Set("Authorization", "vapid t="+jwt+", k="+p.PublicKey())
	c := p.Client
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return resp.StatusCode, nil
}

// vapidJWT signs {aud, exp, sub} with ES256 (RFC 8292).
func (p *Pusher) vapidJWT(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	head := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": time.Now().Add(12 * time.Hour).Unix(), "sub": p.Subject})
	unsigned := head + "." + b64.EncodeToString(claims)
	h := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(rand.Reader, p.key, h[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return unsigned + "." + b64.EncodeToString(sig), nil
}

func hkdf(salt, ikm, info []byte, n int) []byte {
	m := hmac.New(sha256.New, salt)
	m.Write(ikm)
	prk := m.Sum(nil)
	m = hmac.New(sha256.New, prk)
	m.Write(info)
	m.Write([]byte{1})
	return m.Sum(nil)[:n]
}

// Encrypt builds an aes128gcm body for one subscriber (RFC 8291, single record).
func Encrypt(p256dh, auth string, payload []byte) ([]byte, error) {
	as, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	return encrypt(p256dh, auth, payload, as, salt)
}

func encrypt(p256dh, auth string, payload []byte, as *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	uaPubBytes, err := b64.DecodeString(strings.TrimRight(p256dh, "="))
	if err != nil {
		return nil, err
	}
	secret, err := b64.DecodeString(strings.TrimRight(auth, "="))
	if err != nil {
		return nil, err
	}
	uaPub, err := ecdh.P256().NewPublicKey(uaPubBytes)
	if err != nil {
		return nil, err
	}
	shared, err := as.ECDH(uaPub)
	if err != nil {
		return nil, err
	}
	asPub := as.PublicKey().Bytes()
	info := append(append([]byte("WebPush: info\x00"), uaPubBytes...), asPub...)
	ikm := hkdf(secret, shared, info, 32)
	cek := hkdf(salt, ikm, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdf(salt, ikm, []byte("Content-Encoding: nonce\x00"), 12)
	blk, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(blk)
	ct := gcm.Seal(nil, nonce, append(append([]byte{}, payload...), 2), nil)
	out := make([]byte, 0, 21+len(asPub)+len(ct))
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, 4096)
	out = append(out, byte(len(asPub)))
	out = append(out, asPub...)
	return append(out, ct...), nil
}
