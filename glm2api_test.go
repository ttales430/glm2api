package glm2api

import (
	"strings"
	"testing"
	"time"
)

// TestTimestampWithChecksum 校验时间戳算法与官网 JS 逐位一致。
//
// 官网算法：
//
//	A = Date.now().toString()
//	o = A.split("").map(Number)
//	i = o.reduce((e,A)=>e+A, 0) - o[t-2]
//	a = i % 10
//	timestamp = A[:t-2] + a + A[t-1:]
func TestTimestampWithChecksum(t *testing.T) {
	cases := []string{"1790384203102", "1000000000000", "1999999999999", "1700000000001"}
	for _, ms := range cases {
		got := TimestampWithChecksum(ms)

		// 独立复算（避免实现与测试同源）
		digits := make([]int, len(ms))
		sum := 0
		for i := 0; i < len(ms); i++ {
			digits[i] = int(ms[i] - '0')
			sum += digits[i]
		}
		checksum := (sum - digits[len(ms)-2]) % 10
		if checksum < 0 {
			checksum += 10
		}
		want := ms[:len(ms)-2] + string(rune('0'+checksum)) + ms[len(ms)-1:]

		if got != want {
			t.Errorf("TimestampWithChecksum(%s) = %s, want %s", ms, got, want)
		}
		// 不变量：长度、首位、末位不变
		if len(got) != len(ms) {
			t.Errorf("长度改变: %s → %s", ms, got)
		}
		if got[0] != ms[0] || got[len(got)-1] != ms[len(ms)-1] {
			t.Errorf("首/末位被改动: %s → %s", ms, got)
		}
	}
}

// TestTimestampKnownValue 锁定具体样例，防止算法被「自洽地」改错。
//
// 1790384203102：各位和 = 1+7+9+0+3+8+4+2+0+3+1+0+2 = 40
// 倒数第二位 = 0；checksum = (40-0)%10 = 0 → 该位本就是 0，结果不变。
func TestTimestampKnownValue(t *testing.T) {
	if got := TimestampWithChecksum("1790384203102"); got != "1790384203102" {
		t.Errorf("got %s, want 1790384203102", got)
	}
	// 1790384203192：各位和 = 49；倒数第二位 = 9；checksum = (49-9)%10 = 0
	// → 1790384203102
	if got := TimestampWithChecksum("1790384203192"); got != "1790384203102" {
		t.Errorf("got %s, want 1790384203102", got)
	}
}

// TestGenerateSign 校验签名串形状与拼接顺序。
func TestGenerateSign(t *testing.T) {
	now := time.UnixMilli(1790384203102)
	s := GenerateSign(now)

	if len(s.Nonce) != 32 {
		t.Errorf("nonce 长度 = %d, want 32", len(s.Nonce))
	}
	if !isLowerHex(s.Nonce) {
		t.Errorf("nonce 非小写 hex: %s", s.Nonce)
	}
	if len(s.Sign) != 32 {
		t.Errorf("sign 长度 = %d, want 32", len(s.Sign))
	}
	if len(s.Timestamp) != 13 {
		t.Errorf("timestamp 长度 = %d, want 13", len(s.Timestamp))
	}
	want := md5Hex(s.Timestamp + "-" + s.Nonce + "-" + SignSecret)
	if s.Sign != want {
		t.Errorf("sign 拼接顺序不对: got %s, want %s", s.Sign, want)
	}
}

// TestSignSecretUnchanged 签名密钥是协议地基，改动必须是有意的。
func TestSignSecretUnchanged(t *testing.T) {
	const want = "8a1317a7468aa3ad86e997d08f3f31cb"
	if SignSecret != want {
		t.Errorf("SignSecret = %q, want %q（若上游确实变更，请更新 README 并说明实测依据）",
			SignSecret, want)
	}
}

// TestEventDateUTC8 校验签到日期取 UTC+8 当天。
// 关键边界：UTC 16:30 = UTC+8 次日 00:30。
func TestEventDateUTC8(t *testing.T) {
	cases := []struct {
		utc  time.Time
		want string
	}{
		{time.Date(2026, 9, 25, 16, 30, 0, 0, time.UTC), "2026-09-26"},
		{time.Date(2026, 9, 25, 15, 59, 59, 0, time.UTC), "2026-09-25"},
		{time.Date(2026, 9, 26, 23, 59, 59, 0, time.UTC), "2026-09-27"},
	}
	for _, c := range cases {
		if got := EventDate(c.utc); got != c.want {
			t.Errorf("EventDate(%s) = %s, want %s", c.utc.Format(time.RFC3339), got, c.want)
		}
	}
}

// TestRenderPartInitVsFinish 校验同一段落两种 status 的渲染差异。
// 这是协议里最容易写错的地方（init=增量、finish=全文）。
func TestRenderPartInitVsFinish(t *testing.T) {
	// init：增量片段，原样返回
	text, _ := RenderPart(Part{Status: "init", Content: []PartContent{{Type: "text", Text: "你好"}}})
	if text != "你好" {
		t.Errorf("init 段 = %q, want 你好（增量原样）", text)
	}
	// finish：全文，代码块要收尾
	text, _ = RenderPart(Part{Status: "finish", Content: []PartContent{{Type: "code", Code: "print(1)"}}})
	if !strings.Contains(text, "```python") || !strings.Contains(text, "print(1)") {
		t.Errorf("finish 段代码块未包裹: %q", text)
	}
	if !strings.HasSuffix(text, "\n```\n") {
		t.Errorf("finish 段代码块未收尾: %q", text)
	}
}

// TestScoreUnit 积分单位换算（1 积分 = 100 分）必须固定。
//
// 依据：接口 left_score=300000，App 显示 3000 积分。
func TestScoreUnit(t *testing.T) {
	if ScoreUnit != 100 {
		t.Errorf("ScoreUnit = %d, want 100（依据：300000 分 = 3000 积分）", ScoreUnit)
	}
}

// TestParseSSE 用真实抓包的帧序列验证解析与「增量拼接 == finish 全文」。
func TestParseSSE(t *testing.T) {
	// 真实抓包（问「从1数到5」）——逐帧原样抄录，含空格帧
	sse := `event: message
data: {"conversation_id":"c1","status":"init","parts":[]}

event: message
data: {"conversation_id":"c1","status":"init","parts":[{"logic_id":"L1","status":"init","content":[{"type":"text","text":"1"}]}]}

event: message
data: {"conversation_id":"c1","status":"init","parts":[{"logic_id":"L1","status":"init","content":[{"type":"text","text":","}]}]}

event: message
data: {"conversation_id":"c1","status":"init","parts":[{"logic_id":"L1","status":"init","content":[{"type":"text","text":" "}]}]}

event: message
data: {"conversation_id":"c1","status":"init","parts":[{"logic_id":"L1","status":"init","content":[{"type":"text","text":"2"}]}]}

event: message
data: {"conversation_id":"c1","status":"init","parts":[{"logic_id":"L1","status":"init","content":[{"type":"text","text":","}]}]}

event: message
data: {"conversation_id":"c1","status":"init","parts":[{"logic_id":"L1","status":"init","content":[{"type":"text","text":" "}]}]}

event: message
data: {"conversation_id":"c1","status":"init","parts":[{"logic_id":"L1","status":"init","content":[{"type":"text","text":"3"}]}]}

event: message
data: {"conversation_id":"c1","status":"init","parts":[{"logic_id":"L1","status":"init","content":[{"type":"text","text":","}]}]}

event: message
data: {"conversation_id":"c1","status":"init","parts":[{"logic_id":"L1","status":"init","content":[{"type":"text","text":" "}]}]}

event: message
data: {"conversation_id":"c1","status":"init","parts":[{"logic_id":"L1","status":"init","content":[{"type":"text","text":"4, 5"}]}]}

event: message
data: {"conversation_id":"c1","status":"init","parts":[{"logic_id":"L1","status":"finish","content":[{"type":"text","text":"1, 2, 3, 4, 5"}]}]}

event: message
data: {"conversation_id":"c1","status":"finish","parts":[{"logic_id":"L1","status":"finish","content":[{"type":"text","text":"1, 2, 3, 4, 5"}]}]}

`

	var initAcc strings.Builder
	var finalText string
	frames := 0
	err := ParseSSE(strings.NewReader(sse), func(f Frame) error {
		frames++
		for _, p := range f.Parts {
			text, _ := RenderPart(p)
			if p.Status == "finish" {
				finalText = text
			} else {
				initAcc.WriteString(text)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ParseSSE: %v", err)
	}
	if frames != 13 {
		t.Errorf("帧数 = %d, want 13", frames)
	}
	// 核心断言：init 帧拼接 == finish 帧全文
	if got := initAcc.String(); got != finalText {
		t.Errorf("init 拼接 %q != finish 全文 %q\n（若不等，说明对 init/finish 语义理解有误）",
			got, finalText)
	}
	if finalText != "1, 2, 3, 4, 5" {
		t.Errorf("最终文本 = %q, want 1, 2, 3, 4, 5", finalText)
	}
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
