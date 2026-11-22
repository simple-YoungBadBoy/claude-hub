package webpush

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// decrypt is the browser's side of RFC 8291.
func decrypt(t *testing.T, ua *ecdh.PrivateKey, secret, body []byte) []byte {
	salt, idlen := body[:16], int(body[20])
	asPub := body[21 : 21+idlen]
	ct := body[21+idlen:]
	as, err := ecdh.P256().NewPublicKey(asPub)
	if err != nil {
		t.Fatal(err)
	}
	shared, _ := ua.ECDH(as)
	info := append(append([]byte("WebPush: info\x00"), ua.PublicKey().Bytes()...), asPub...)
	ikm := hkdf(secret, shared, info, 32)
	cek := hkdf(salt, ikm, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdf(salt, ikm, []byte("Content-Encoding: nonce\x00"), 12)
	blk, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(blk)
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pt[len(pt)-1] != 2 {
		t.Fatal("missing last-record delimiter")
	}
	return pt[:len(pt)-1]
}

func TestEncryptRoundTrip(t *testing.T) {
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	secret := make([]byte, 16)
	rand.Read(secret)
	body, err := Encrypt(b64.EncodeToString(ua.PublicKey().Bytes()), b64.EncodeToString(secret), []byte(`{"title":"完成"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := decrypt(t, ua, secret, body); string(got) != `{"title":"完成"}` {
		t.Fatalf("got %q", got)
	}
}

// RFC 8291 section 5 test vector.
func TestEncryptRFCVector(t *testing.T) {
	d := func(s string) []byte { b, _ := b64.DecodeString(s); return b }
	asPriv := d("yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw")
	salt := d("DGv6ra1nlYgDCS1FRnbzlw")
	as, err := ecdh.P256().NewPrivateKey(asPriv)
	if err != nil {
		t.Fatal(err)
	}
	got, err := encrypt("BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4", "BTBZMqHH6r4Tts7J_aSIgg", []byte("When I grow up, I want to be a watermelon"), as, salt)
	if err != nil {
		t.Fatal(err)
	}
	want := d("DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN")
	if !bytes.Equal(got, want) {
		t.Fatalf("mismatch with RFC 8291 vector\n got %s\nwant %s", b64.EncodeToString(got), b64.EncodeToString(want))
	}
}

func TestSendAllSignsAndDropsGone(t *testing.T) {
	dir := t.TempDir()
	p, err := Open(filepath.Join(dir, "vapid.pem"), filepath.Join(dir, "push.json"))
	if err != nil {
		t.Fatal(err)
	}
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	secret := make([]byte, 16)
	rand.Read(secret)
	var got []byte
	var authz string
	gone := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gone {
			w.WriteHeader(http.StatusGone)
			return
		}
		got, _ = io.ReadAll(r.Body)
		authz = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	p.Client = srv.Client()
	allowedHosts = append(allowedHosts, "127.0.0.1")
	var s Subscription
	s.Endpoint = srv.URL + "/push/abc"
	s.Keys.P256dh = b64.EncodeToString(ua.PublicKey().Bytes())
	s.Keys.Auth = b64.EncodeToString(secret)
	if err := p.Add(s); err != nil {
		t.Fatal(err)
	}
	p.SendAll(Message{Title: "✅ Claude 完成了", Body: "cc-x", Session: "cc-x"})
	var m Message
	if err := json.Unmarshal(decrypt(t, ua, secret, got), &m); err != nil || m.Session != "cc-x" {
		t.Fatalf("payload %+v %v", m, err)
	}
	// VAPID: a valid ES256 JWT for the endpoint's origin, signed by our key
	parts := strings.Split(strings.TrimPrefix(strings.SplitN(authz, ",", 2)[0], "vapid t="), ".")
	if len(parts) != 3 || !strings.Contains(authz, "k="+p.PublicKey()) {
		t.Fatalf("authorization %q", authz)
	}
	claims, _ := b64.DecodeString(parts[1])
	if !strings.Contains(string(claims), `"aud":"`+srv.URL+`"`) {
		t.Fatalf("claims %s", claims)
	}
	sig, _ := b64.DecodeString(parts[2])
	h := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(&p.key.PublicKey, h[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("bad signature")
	}
	// the key survives a restart
	p2, _ := Open(filepath.Join(dir, "vapid.pem"), filepath.Join(dir, "push.json"))
	if p2.PublicKey() != p.PublicKey() || p2.Count() != 1 {
		t.Fatal("key or subscriptions not persisted")
	}
	gone = true
	p.SendAll(Message{Title: "x"})
	if p.Count() != 0 {
		t.Fatal("410 Gone subscription kept")
	}
}

func TestAddRejectsOtherHosts(t *testing.T) {
	dir := t.TempDir()
	p, _ := Open(filepath.Join(dir, "k"), filepath.Join(dir, "s"))
	var s Subscription
	s.Endpoint = "https://evil.example/x"
	if p.Add(s) == nil {
		t.Fatal("arbitrary endpoint accepted")
	}
	s.Endpoint = "http://web.push.apple.com/x"
	if p.Add(s) == nil {
		t.Fatal("plain http accepted")
	}
}
