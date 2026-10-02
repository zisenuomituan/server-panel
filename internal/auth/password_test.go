package auth

import "testing"

func TestHashAndCheck(t *testing.T) {
	hash, err := HashPassword("hunter2hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword("hunter2hunter2", hash) {
		t.Fatal("正确密码应校验通过")
	}
	if CheckPassword("wrongpassword", hash) {
		t.Fatal("错误密码不应通过")
	}
}

func TestHashIsSalted(t *testing.T) {
	a, _ := HashPassword("samepassword")
	b, _ := HashPassword("samepassword")
	if a == b {
		t.Fatal("相同密码两次哈希应不同（有随机盐）")
	}
}
