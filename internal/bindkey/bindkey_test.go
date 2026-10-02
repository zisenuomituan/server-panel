package bindkey

import "testing"

func TestNewAndValid(t *testing.T) {
	k, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if len(k) != 32 {
		t.Fatalf("长度应为 32，实际 %d", len(k))
	}
	if !Valid(k) {
		t.Fatalf("生成的密钥应通过校验: %s", k)
	}
}

func TestNewIsRandom(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		k, _ := New()
		if seen[k] {
			t.Fatalf("出现重复密钥: %s", k)
		}
		seen[k] = true
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"abcd-efgh": "ABCDEFGH",
		"ABCD EFGH": "ABCDEFGH",
		"io12":      "1012", // I/O 被纠正为 1/0
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestHashStable(t *testing.T) {
	k := "ABCDEFGHJKLMNPQRSTVWXYZ012345"
	if Hash(k) != Hash(k) {
		t.Fatal("同一密钥的哈希应一致")
	}
	if Hash(k) == Hash(k+"X") {
		t.Fatal("不同密钥的哈希不应相同")
	}
}

func TestFormat(t *testing.T) {
	got := Format("ABCDEFGH")
	if got != "ABCD-EFGH" {
		t.Fatalf("Format = %q", got)
	}
}
