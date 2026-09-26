# glm2api — 智谱清言（chatglm.cn）协议逆向成果

> **本仓库是智谱清言网页版私有接口的协议逆向研究成果**，为
> [wild-work](https://github.com/rockswang/wild-work) 等聚合工具的 `glm` 渠道提供
> 协议依据（对应 wild-work README「致谢」表中各渠道的上游项目角色）。

---

## 1. 这是什么

智谱清言（chatglm.cn）网页版**没有官方开放 API**（那是 `open.bigmodel.cn`，按 token 计费）。
本仓库记录**网页版私有接口**的协议细节，供自用工具集成：

| 内容 | 说明 |
|------|------|
| **签名算法** | `X-Sign = md5(timestamp-nonce-SECRET)`，含时间戳校验位算法 |
| **认证流程** | `chatglm_refresh_token` → `access_token`（含轮换语义） |
| **对话协议** | `/backend-api/assistant/stream` 的 SSE 语义与 `assistant_id`/`chat_mode` 映射 |
| **积分机制** | `member-info` 的积分字段与单位换算 |
| **智能体清单** | 实测可用的 assistant_id 及失效情况 |

> ⚠️ **本仓库只做协议记录与最小参考实现，不提供可直接部署的服务。**
> 它存在的意义是让下游聚合工具（如 wild-work）能引用一份公开的协议来源。

---

## 2. 协议要点（2026-09-26 实测）

### 2.1 签名（地基）

清言网页版所有私有接口都要求一组签名头，缺一即 `400 bad request(40001)`：

```
X-Timestamp  毫秒时间戳，倒数第二位被替换成校验位
X-Nonce      32 位随机 hex
X-Sign       md5("<timestamp>-<nonce>-<SIGN_SECRET>")
X-Device-Id  设备标识
X-Request-Id 请求标识
```

**时间戳算法**（复刻官网 JS，勿改）：

```
取 Date.now() 的十进制字符串 A，令 t = len(A)；
校验位 = (A 各位数字之和 - A[t-2]) % 10；
结果 = A[:t-2] + 校验位 + A[t-1:]
```

即把**倒数第二位数字**替换为校验位，最后一位保持不变。

**SIGN_SECRET**：`8a1317a7468aa3ad86e997d08f3f31cb`（客户端硬编码）

**实测判据**（变量隔离干净）：

| 请求 | 响应 | 含义 |
|------|------|------|
| 带签名 → `user/refresh` | `401` `unauthorized user(40102)` | 签名**通过**，卡认证层 |
| 不带签名 → 同一接口 | `400` `bad request(40001)` | 签名层**拒绝** |

### 2.2 接口清单

| 用途 | 方法 | 路径 |
|------|------|------|
| 刷新 token | POST | `/chatglm/user-api/user/refresh` |
| 用户信息 | GET | `/chatglm/user-api/user/info` |
| 会员与积分 | GET | `/chatglm/member-api/member/member_info` |
| 对话（SSE） | POST | `/chatglm/backend-api/assistant/stream` |
| 删除会话 | POST | `/chatglm/backend-api/assistant/conversation/delete` |

统一信封 `{status, message, result, rid}`，`status != 0` 即错误。

**关键：`refresh_token` 每次刷新都会轮换**，旧值立即作废 → 必须落盘，
否则下次启动用旧 token，账号永久失效。

### 2.3 对话请求体

```json
{
  "assistant_id": "65940acff94777010aa6b796",
  "conversation_id": "",
  "project_id": "",
  "chat_type": "user_chat",
  "messages": [{"role":"user","content":[{"type":"text","text":"<合并后的全部上下文>"}]}],
  "meta_data": {"chat_mode": "", "platform": "pc", "if_plus_model": true, ...}
}
```

**两个关键点**：

1. **多轮上下文必须由客户端合并进单条 user 消息**。清言服务端无状态，
   用 `<|user|>` / `<|assistant|>` / `<|sytstem|>`（注意官方拼写就是 `sytstem`）标记角色边界。
2. **`chat_mode` 决定模式**：`""` 普通、`"zero"` 推理、`"deep_research"` 沉思、
   `"ppt"` / `"video"` 特殊智能体。

### 2.4 模型映射（`assistant_id` + `chat_mode`）

清言**没有公开的模型列表接口**（官方 App 客户端硬编码）。`assistant_id` 即官方「智能体」ID。

| 客户端名 | assistant_id | chat_mode | 实测 |
|---------|-------------|-----------|------|
| chatglm（默认） | `65940acff94777010aa6b796` | — | ✅ |
| chatglm-think | 同上 | `zero` | ✅ |
| chatglm-deepresearch | 同上 | `deep_research` | ✅ |
| search（AI搜索） | `659e54b1b8006379b4b2abd6` | — | ✅ |
| ppt（清言PPT） | `670e3c3e119b48fe5a851149` | `ppt` | ✅ |
| video（视频助手） | `668d03b2e99d661ed3c32516` | `video` | ✅ |
| ~~aidraw（AI画图）~~ | `65a232c082ff90a2ad2f15e2` | — | ❌ 需 `cogview` 参数 |
| ~~doc（AI阅读）~~ | `658a7988b8a9a98d38725745` | — | ❌ 需上传文件 |
| ~~study（学习搭子）~~ | `68f0b8c110eea3e78b0e0e5e` | — | ❌ 上游 `10025 internal server error` |

**另外支持直填任意 24 位 hex 智能体 ID**（清言原生语义）。

### 2.5 SSE 语义：`init` 帧是增量，`finish` 帧是全文

> ⚠️ **这是本协议最容易写错的地方。**

`part.content[].text / .think` 的语义**取决于 `part.status`**：

| `part.status` | `text`/`think` 的含义 |
|---|---|
| `init` | **增量片段**（delta），直接透传 |
| `finish` | 该段落的**完整全文**，不可重复发出 |

实测帧序列（问「从1数到5」）：

```
帧2  status=init   part.status=init   text="1"
帧3  status=init   part.status=init   text=","
帧4  status=init   part.status=init   text=" "
...
帧12 status=init   part.status=finish text="1, 2, 3, 4, 5"   ← 完整全文
帧13 status=finish part.status=finish text="1, 2, 3, 4, 5"
```

即 **init 帧片段按序拼接 == finish 帧全文**。think 段同规则。
段落按 `logic_id` 稳定标识。

**已知社区实现的错误**：`LLM-Red-Team/glm-free-api` 的**流式路径把 `parts` 当作全量快照**，
用 `substring(sentContent.length)` 求差量——在 delta 语义下会**丢字**。
（其非流式路径靠 finish 帧碰巧正确，故注释里误写成「全量快照」。**不要照抄。**）

### 2.6 积分机制

```json
GET /chatglm/member-api/member/member_info
{
  "left_score": 447224,        // 积分余额，单位「分」
  "true_left_score": 0,        // 恒为 0（疑似未启用字段）
  "left_token": 17667093,      // 剩余 token 额度
  "score_rule": "免费用户，登录赠送200积分/天"
}
```

**单位换算**：`left_score` 单位是「分」，**1 积分 = 100 分**。
依据：接口返回 300000，App 界面显示 3000 积分，恰好差 100 倍。

**关键结论**：

1. **清言没有「签到领取」接口**。积分由**服务端按天被动发放**（登录赠送 200/天），
   用户无需也无法主动领取。这与 Qoder 的 campaigns 签到是**完全不同的机制**。
2. App 里的「做任务，赚积分」是**任务激励**，每日任务「与伙伴对话」
   **发一条消息即算完成**，同样没有领取步骤。
3. 故「保活」= 发一条最小对话，既满足每日任务又维持账号活跃。

### 2.7 模型版本：客户端无法选择

**实测**：主对话在 `chat_mode` 为空/`zero`/`deep_research` 三种情况下，
服务端上报的 `model` 字段**恒为 `moe_53f`**；AI搜索为 `ai-search`；
视频助手/清言PPT 为 `all-tools-glms-glms-v2`。

**结论**：清言网页版**没有「选择模型版本」这个能力**。模型由服务端按 `assistant_id` 分配，
客户端不传版本号也无法传。`moe_53f` 是服务端内部代号（`moe` = MoE，`53` 很可能指 5.3），
但它是**观测值**不是**可选项**。

> 想要 GLM-5.3 的具体版本控制，只能走**官方开放平台**（open.bigmodel.cn）。

---

## 3. 登录凭据获取

凭据是浏览器 Cookie 里的 `chatglm_refresh_token`。

**不要试图直接读浏览器 Cookie 数据库**（实测排除）：
- Edge/Chrome 运行时对 `User Data/<Profile>/Network/Cookies` 持**独占锁**
  （实测 20 个进程），既不能读也不能复制
- Cookie 值为 **DPAPI + AES-GCM 加密**

**可行方案**：经 CDP（Chrome DevTools Protocol）拉起**独立 profile** 的浏览器，
用户正常登录后用 `Network.getAllCookies` 读回。

> ⚠️ **陷阱**：清言对**全新访客会自动下发** `chatglm_refresh_token`（实测 423 字符），
> 该 token 调 `user/refresh` 会被拒绝。故「Cookie 出现」≠「用户已登录」——
> **必须以「凭据验证通过」为完成判据**，否则会抓到一个永远不可用的访客账号。

---

## 4. 与 wild-work 的关系

本仓库对应 wild-work README「致谢」表中各渠道上游项目的角色：

| 渠道 | wild-work 引用的上游项目 |
|------|------------------------|
| WorkBuddy(CodeBuddy) | Sliverkiss/workbuddy2api |
| TraeWork | (见 wild-work README) |
| Qoder | qoder2api |
| 千问办公 | (见 wild-work README) |
| OpenCodeZen | (见 wild-work README) |
| **智谱清言** | **本仓库 glm2api** |

即：wild-work 只做**多渠道路由、账号池调度、协议适配与界面封装**；
本仓库提供智谱清言渠道的**协议逆向成果**。

---

## 5. 免责声明

**本项目仅供学习和研究使用。**

- 本仓库基于对智谱清言（chatglm.cn）**未公开接口的逆向工程**，非官方支持、无任何授权。
- 上游随时可能调整接口、加密方式或风控策略，导致**部分或全部功能立即失效**。
- 使用相关实现可能违反上游平台服务条款，存在账号被**限制功能、降低额度、暂时或永久封禁**的风险。
  **强烈建议使用小号或可承受损失的账号，切勿使用主力账号。**
- **仅限自用**：不要对外提供服务、不要商用、不要分享给他人。
- 建议保持服务只监听 `127.0.0.1`，不要暴露到公网。
- 控制请求频率：保活 + 签到每日两次即可，不要高频轰炸。

---

## 6. 协议变更记录

| 日期 | 变更 |
|------|------|
| 2026-09-26 | 首次记录：签名算法、认证流程、对话 SSE 语义、积分机制、智能体可用性实测 |
