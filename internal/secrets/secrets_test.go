package secrets

import (
	"strings"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	c := NewCipher("correct horse")
	got, enc, err := c.Seal("s3cr3t-token")
	if err != nil || !enc {
		t.Fatalf("seal: enc=%v err=%v", enc, err)
	}
	if got == "s3cr3t-token" || !IsEncrypted(got) {
		t.Fatalf("密文异常: %q", got)
	}
	back, err := c.Open(got)
	if err != nil || back != "s3cr3t-token" {
		t.Fatalf("解密往返: got=%q err=%v", back, err)
	}
}

func TestTwoSealsDifferBySaltNonce(t *testing.T) {
	c := NewCipher("p")
	a, _, _ := c.Seal("v")
	b, _, _ := c.Seal("v")
	if a == b {
		t.Fatal("相同明文两次加密应因随机盐/nonce 而不同")
	}
	if x, _ := c.Open(a); x != "v" {
		t.Fatal("a 解密失败")
	}
}

func TestOpenPassthroughPlaintext(t *testing.T) {
	c := NewCipher("p")
	if got, err := c.Open("plain-value"); err != nil || got != "plain-value" {
		t.Fatalf("非密文应原样返回: got=%q err=%v", got, err)
	}
}

func TestWrongPassphraseFails(t *testing.T) {
	blob, _, err := NewCipher("good").Seal("secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCipher("bad").Open(blob); err == nil {
		t.Fatal("错误口令应解密失败")
	}
}

func TestSealNoPassphrasePassthrough(t *testing.T) {
	c := NewCipher("")
	got, enc, err := c.Seal("secret")
	if err != nil || enc || got != "secret" {
		t.Fatalf("无口令应原样返回且标记未加密: got=%q enc=%v err=%v", got, enc, err)
	}
}

func TestDecryptGarbageFails(t *testing.T) {
	if _, err := NewCipher("p").Open(prefix + "@@@notbase64@@@"); err == nil {
		t.Fatal("非法 base64 应报错")
	}
	if _, err := NewCipher("p").Open(prefix + strings.Repeat("A", 8)); err == nil {
		t.Fatal("短密文应报错")
	}
}
