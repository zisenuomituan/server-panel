package sshpool

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"golang.org/x/crypto/ssh"
)

type memKnownHosts struct {
	m map[string]string
}

func (k *memKnownHosts) HostKey(host string) (string, bool, error) {
	v, ok := k.m[host]
	return v, ok, nil
}

func (k *memKnownHosts) SaveHostKey(host, key string) error {
	k.m[host] = key
	return nil
}

func newKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestCheckTOFU(t *testing.T) {
	kh := &memKnownHosts{m: map[string]string{}}
	key := newKey(t)

	// 第一次：信任并记录
	if err := CheckTOFU(kh, "host:22", key); err != nil {
		t.Fatalf("第一次应通过: %v", err)
	}
	// 再连：密钥一致
	if err := CheckTOFU(kh, "host:22", key); err != nil {
		t.Fatalf("密钥一致应通过: %v", err)
	}
	// 换了密钥：应报错
	if err := CheckTOFU(kh, "host:22", newKey(t)); err == nil {
		t.Fatal("主机密钥变化应报错")
	}
	// 不同主机互不影响
	if err := CheckTOFU(kh, "other:22", key); err != nil {
		t.Fatalf("不同主机应通过: %v", err)
	}
}
