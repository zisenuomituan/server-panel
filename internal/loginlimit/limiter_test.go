package loginlimit

import (
	"testing"
	"time"
)

func TestLockAfterMaxFailures(t *testing.T) {
	now := time.Now()
	l := New(3, time.Minute, 5*time.Minute)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if ok, _ := l.Allowed("ip:1"); !ok {
			t.Fatalf("第 %d 次不该被锁", i+1)
		}
		l.Fail("ip:1")
	}

	ok, wait := l.Allowed("ip:1")
	if ok {
		t.Fatal("达到上限后应被锁定")
	}
	if wait <= 0 || wait > 5*time.Minute {
		t.Fatalf("剩余锁定时间不对: %v", wait)
	}
}

func TestSuccessClears(t *testing.T) {
	l := New(3, time.Minute, 5*time.Minute)
	l.Fail("u:a")
	l.Fail("u:a")
	l.Success("u:a")
	l.Fail("u:a")
	if ok, _ := l.Allowed("u:a"); !ok {
		t.Fatal("成功后计数应清零，不该被锁")
	}
}

func TestLockExpires(t *testing.T) {
	now := time.Now()
	l := New(2, time.Minute, 5*time.Minute)
	l.now = func() time.Time { return now }

	l.Fail("ip:1")
	l.Fail("ip:1")
	if ok, _ := l.Allowed("ip:1"); ok {
		t.Fatal("应处于锁定中")
	}
	now = now.Add(6 * time.Minute)
	if ok, _ := l.Allowed("ip:1"); !ok {
		t.Fatal("锁定过期后应放行")
	}
}

func TestWindowResets(t *testing.T) {
	now := time.Now()
	l := New(2, time.Minute, 5*time.Minute)
	l.now = func() time.Time { return now }

	l.Fail("ip:1")
	now = now.Add(2 * time.Minute) // 超过统计窗口
	l.Fail("ip:1")
	if ok, _ := l.Allowed("ip:1"); !ok {
		t.Fatal("窗口过期后计数应重新开始")
	}
}
