// Package glm2api 智谱清言（chatglm.cn）协议的最小自包含参考实现。
//
// 本文件只依赖标准库，可直接复制到任何 Go 项目使用。
// 它存在的意义是给下游聚合工具（如 wild-work）一份可引用的协议来源。
//
// 协议细节见仓库 README。核心要点：
//   - 签名：X-Sign = md5(timestamp-nonce-SECRET)，时间戳倒数第二位是校验位
//   - 认证：refresh_token → access_token，且 refresh_token 会轮换
//   - 对话：SSE 的 part.status=init 是增量、=finish 是全文
package glm2api

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// 常量
// ---------------------------------------------------------------------------

const (
	// Base 上游站点根。
	Base = "https://chatglm.cn"
	// Origin 清言对私有接口校验 Origin/Referer。
	Origin = "https://chatglm.cn"

	// SignSecret 客户端内硬编码的签名密钥。
	//
	// 2026-09-26 实测仍有效：带签名 → 401 unauthorized user（认证层拒绝）；
	// 不带签名 → 400 bad request（签名层拒绝）。两者响应不同即证明签名通过。
	SignSecret = "8a1317a7468aa3ad86e997d08f3f31cb"

	// DefaultAssistantID 清言主对话（ChatGLM）的 assistant_id。
	DefaultAssistantID = "65940acff94777010aa6b796"

	// UserAgent 伪装 Edge 143（与清言网页版实测一致）。
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36 Edg/143.0.0.0"
)

// 接口路径。
const (
	EpRefresh    = "/chatglm/user-api/user/refresh"
	EpUserInfo   = "/chatglm/user-api/user/info"
	EpMemberInfo = "/chatglm/member-api/member/member_info"
	EpChat       = "/chatglm/backend-api/assistant/stream"
	EpDeleteConv = "/chatglm/backend-api/assistant/conversation/delete"
)

// utc8 清言所有日期口径都是 UTC+8 墙钟。
var utc8 = time.FixedZone("UTC+8", 8*60*60)

// ---------------------------------------------------------------------------
// 签名
// ---------------------------------------------------------------------------

// Sign 一次签名所需的三个值。
type Sign struct {
	Timestamp string
	Nonce     string
	Sign      string
}

// GenerateSign 按官网算法生成签名。
func GenerateSign(now time.Time) Sign {
	ms := strconv.FormatInt(now.UnixMilli(), 10)
	ts := TimestampWithChecksum(ms)
	nonce := RandomHex(32)
	return Sign{
		Timestamp: ts,
		Nonce:     nonce,
		Sign:      md5Hex(ts + "-" + nonce + "-" + SignSecret),
	}
}

// TimestampWithChecksum 复刻官网时间戳算法：把倒数第二位替换为校验位。
//
// 官网 JS：
//
//	A = Date.now().toString()
//	o = A.split("").map(Number)
//	i = o.reduce((e,A)=>e+A, 0) - o[t-2]     // 各位和 减去 倒数第二位
//	timestamp = A[:t-2] + (i % 10) + A[t-1:]
func TimestampWithChecksum(ms string) string {
	n := len(ms)
	if n < 2 {
		return ms
	}
	sum := 0
	for i := 0; i < n; i++ {
		c := ms[i]
		if c < '0' || c > '9' {
			return ms // 非纯数字，放弃改写（防御性）
		}
		sum += int(c - '0')
	}
	second := int(ms[n-2] - '0')
	checksum := (sum - second) % 10
	if checksum < 0 {
		checksum += 10
	}
	return ms[:n-2] + strconv.Itoa(checksum) + ms[n-1:]
}

// md5Hex 返回小写 hex 的 MD5。
func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// RandomHex 返回 n 位小写 hex 随机串。
func RandomHex(n int) string {
	buf := make([]byte, (n+1)/2)
	if _, err := rand.Read(buf); err != nil {
		fallback := md5.Sum([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		return hex.EncodeToString(fallback[:])[:n]
	}
	return hex.EncodeToString(buf)[:n]
}

// ApplyHeaders 注入清言私有接口要求的伪装头 + 签名头。
// token 非空时附带 Authorization。accept 传 "text/event-stream" 用于对话流。
func ApplyHeaders(req *http.Request, token, accept string) {
	h := req.Header
	h.Set("Content-Type", "application/json")
	if accept == "" {
		accept = "application/json, text/plain, */*"
	}
	h.Set("Accept", accept)
	h.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	h.Set("App-Name", "chatglm")
	h.Set("Cache-Control", "no-cache")
	h.Set("Pragma", "no-cache")
	h.Set("Origin", Origin)
	h.Set("Referer", Origin+"/main/alltoolsdetail")
	h.Set("User-Agent", UserAgent)
	h.Set("X-App-Fr", "browser_extension")
	h.Set("X-App-Platform", "pc")
	h.Set("X-App-Version", "0.0.1")
	h.Set("X-Device-Brand", "")
	h.Set("X-Device-Model", "")
	h.Set("X-Lang", "zh")
	h.Set("Sec-Ch-Ua", `"Microsoft Edge";v="143", "Chromium";v="143", "Not A(Brand";v="24"`)
	h.Set("Sec-Ch-Ua-Mobile", "?0")
	h.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	h.Set("Sec-Fetch-Dest", "empty")
	h.Set("Sec-Fetch-Mode", "cors")
	h.Set("Sec-Fetch-Site", "same-origin")
	h.Set("Priority", "u=1, i")

	s := GenerateSign(time.Now())
	h.Set("X-Device-Id", RandomHex(32))
	h.Set("X-Request-Id", RandomHex(32))
	h.Set("X-Nonce", s.Nonce)
	h.Set("X-Sign", s.Sign)
	h.Set("X-Timestamp", s.Timestamp)
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
}

// ---------------------------------------------------------------------------
// 信封与认证
// ---------------------------------------------------------------------------

// Envelope 清言统一响应信封。
type Envelope struct {
	Status  int             `json:"status"`
	Message string          `json:"message"`
	Result  json.RawMessage `json:"result"`
	Rid     string          `json:"rid"`
}

// ErrText 返回可读错误文案。
func (e *Envelope) ErrText() string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = "unknown error"
	}
	return fmt.Sprintf("status=%d %s", e.Status, msg)
}

// Token 一次刷新得到的凭据。
type Token struct {
	AccessToken  string
	RefreshToken string // 轮换后的新值，必须落盘
	ExpiresAt    int64
	UID          string
}

// Refresh 用 refresh_token 换 access_token。
//
// ⚠️ **返回的 RefreshToken 是新值，必须落盘**——清言每次刷新都轮换 refresh_token，
// 旧值立即作废。不落盘 = 下次启动用旧 token → 账号永久失效。
func Refresh(client *http.Client, refreshToken string) (Token, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return Token{}, fmt.Errorf("glm2api: refresh_token 为空")
	}
	req, err := http.NewRequest(http.MethodPost, Base+EpRefresh, strings.NewReader("{}"))
	if err != nil {
		return Token{}, err
	}
	ApplyHeaders(req, refreshToken, "")
	resp, err := client.Do(req)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Token{}, fmt.Errorf("glm2api: 解析刷新响应失败: %w", err)
	}
	if resp.StatusCode >= 400 || env.Status != 0 {
		return Token{}, fmt.Errorf("glm2api: 刷新失败 http=%d %s", resp.StatusCode, env.ErrText())
	}
	var r struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		UserID       string `json:"user_id"`
		ExpiresIn    int64  `json:"expires_in"`
		IsGuest      bool   `json:"is_guest"`
	}
	if err := json.Unmarshal(env.Result, &r); err != nil {
		return Token{}, fmt.Errorf("glm2api: 解析刷新结果失败: %w", err)
	}
	if r.AccessToken == "" {
		return Token{}, fmt.Errorf("glm2api: 刷新响应缺少 access_token")
	}
	if r.IsGuest {
		return Token{}, fmt.Errorf("glm2api: 访客账号不可用，请登录真实账号")
	}
	exp := int64(0)
	if r.ExpiresIn > 0 {
		exp = time.Now().Add(time.Duration(r.ExpiresIn) * time.Second).Unix()
	}
	return Token{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		ExpiresAt:    exp,
		UID:          r.UserID,
	}, nil
}

// ---------------------------------------------------------------------------
// 积分
// ---------------------------------------------------------------------------

// ScoreUnit 上游 left_score 的单位换算：1 积分 = 100 分。
//
// 依据（2026-09-26 实测）：接口返回 300000，而清言 App 界面显示 3000 积分。
const ScoreUnit = 100

// MemberInfo 会员与积分信息。
type MemberInfo struct {
	Score     int64  // 积分（已换算：raw / 100）
	RawScore  int64  // 上游原始值（单位「分」）
	LeftToken int64
	ScoreRule string
	IsMember  bool
}

// FetchMemberInfo 拉取积分与会员信息。
//
// 这是清言**唯一**能拿到积分余额的接口。
func FetchMemberInfo(client *http.Client, accessToken string) (MemberInfo, error) {
	req, err := http.NewRequest(http.MethodGet, Base+EpMemberInfo, nil)
	if err != nil {
		return MemberInfo{}, err
	}
	ApplyHeaders(req, accessToken, "")
	resp, err := client.Do(req)
	if err != nil {
		return MemberInfo{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return MemberInfo{}, err
	}
	if resp.StatusCode >= 400 || env.Status != 0 {
		return MemberInfo{}, fmt.Errorf("glm2api: 取积分失败 http=%d %s", resp.StatusCode, env.ErrText())
	}
	var m struct {
		LeftScore int64  `json:"left_score"`
		LeftToken int64  `json:"left_token"`
		ScoreRule string `json:"score_rule"`
		IsMember  bool   `json:"is_member"`
	}
	if err := json.Unmarshal(env.Result, &m); err != nil {
		return MemberInfo{}, err
	}
	return MemberInfo{
		Score:     m.LeftScore / ScoreUnit,
		RawScore:  m.LeftScore,
		LeftToken: m.LeftToken,
		ScoreRule: m.ScoreRule,
		IsMember:  m.IsMember,
	}, nil
}

// ---------------------------------------------------------------------------
// SSE
// ---------------------------------------------------------------------------

// Part 一个段落。同一段落在流中由 logic_id 稳定标识。
type Part struct {
	LogicID string        `json:"logic_id"`
	Status  string        `json:"status"` // "init"=内容为增量；"finish"=内容为该段落全文
	Content []PartContent `json:"content"`
}

// PartContent 段落内的一个内容项。
type PartContent struct {
	Type  string `json:"type"`  // text / think / code / image / ...
	Text  string `json:"text"`
	Think string `json:"think"`
	Code  string `json:"code"`
}

// Frame 上游单帧。
type Frame struct {
	ConversationID string `json:"conversation_id"`
	Status         string `json:"status"`
	Parts          []Part `json:"parts"`
}

// ParseSSE 逐帧解析上游 SSE。
func ParseSSE(r io.Reader, onFrame func(Frame) error) error {
	dec := json.NewDecoder(r)
	_ = dec // 占位：实际用下面的逐行解析

	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 4096)
	var acc strings.Builder
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				idx := indexByte(buf, '\n')
				if idx < 0 {
					break
				}
				line := string(buf[:idx])
				buf = buf[idx+1:]
				line = strings.TrimRight(line, "\r")
				if line == "" {
					if acc.Len() > 0 {
						var f Frame
						if jerr := json.Unmarshal([]byte(acc.String()), &f); jerr == nil {
							if ferr := onFrame(f); ferr != nil {
								return ferr
							}
						}
						acc.Reset()
					}
					continue
				}
				if strings.HasPrefix(line, "data:") {
					v := strings.TrimPrefix(line, "data:")
					v = strings.TrimPrefix(v, " ")
					if acc.Len() > 0 {
						acc.WriteString("\n")
					}
					acc.WriteString(v)
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// indexByte 在字节切片里找第一个 b，返回下标或 -1。
func indexByte(s []byte, b byte) int {
	for i := range s {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// RenderPart 渲染段落的可见正文与思考文本。
//
// 对 init 帧，返回值即本帧增量；对 finish 帧，返回值即该段落全文。
func RenderPart(p Part) (text, think string) {
	var tb, rb strings.Builder
	finished := p.Status == "finish"
	for _, c := range p.Content {
		switch c.Type {
		case "text":
			tb.WriteString(c.Text)
		case "think":
			rb.WriteString(c.Think)
		case "code":
			tb.WriteString("```python\n" + c.Code)
			if finished {
				tb.WriteString("\n```\n")
			}
		}
	}
	return tb.String(), rb.String()
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

// EventDate 返回清言签到类接口要求的 event_date（UTC+8 当天，YYYY-MM-DD）。
func EventDate(now time.Time) string {
	return now.In(utc8).Format("2006-01-02")
}
