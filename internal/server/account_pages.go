package server

import (
	"html/template"
	"net/http"
)

// sharedPageStyle is reused across the small account-action pages. Colors are
// driven by CSS variables so the pages follow the operating-system light/dark
// preference automatically.
const sharedPageStyle = `:root { --bg:#0f172a; --surface:#111827; --surface-2:#0b1220; --border:#1f2937; --text:#e2e8f0; --text-soft:#cbd5e1; }
		@media (prefers-color-scheme: light) { :root { --bg:#f1f5f9; --surface:#ffffff; --surface-2:#f8fafc; --border:#e2e8f0; --text:#1e293b; --text-soft:#334155; } }
		body { font-family: "Segoe UI", sans-serif; background: var(--bg); color: var(--text); margin: 0; padding: 0; display: flex; align-items: center; justify-content: center; height: 100vh; }
		.card { background: var(--surface); padding: 32px; border-radius: 12px; box-shadow: 0 10px 30px rgba(0,0,0,0.20); width: 360px; }
		h1 { margin: 0 0 12px 0; font-size: 22px; }
		label { display: block; margin-top: 12px; color: var(--text-soft); }
		input { width: 100%; padding: 10px; margin-top: 6px; border-radius: 8px; border: 1px solid var(--border); background: var(--surface-2); color: var(--text); box-sizing: border-box; }
		button { width: 100%; margin-top: 18px; padding: 12px; border: none; border-radius: 10px; background: linear-gradient(120deg,#22d3ee,#818cf8); color: #0b1220; font-weight: 700; cursor: pointer; }
		button:hover { filter: brightness(1.05); }
		.error { color: #f87171; margin-top: 10px; min-height: 20px; }
		.success { color: #34d399; margin-top: 10px; min-height: 20px; }
		.link { text-align: center; margin-top: 16px; }
		.link a { color: #22d3ee; text-decoration: none; }
		.link a:hover { text-decoration: underline; }
		.lang-switch { position: absolute; top: 16px; right: 16px; }
		.lang-switch select { padding: 6px 10px; border-radius: 6px; border: 1px solid var(--border); background: var(--surface); color: var(--text); cursor: pointer; }`

var appForgotPasswordTemplate = template.Must(template.New("appForgot").Parse(`<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>NekoLc Forgot Password</title>
	<style>` + sharedPageStyle + `</style>
</head>
<body>
	<div class="lang-switch">
		<select id="langSelect" onchange="changeLang()">
			<option value="en">English</option>
			<option value="zh-hans">简体中文</option>
			<option value="zh-hant">繁體中文</option>
		</select>
	</div>
	<div class="card">
		<h1 id="title">Forgot Password</h1>
		<label id="lbl-email">Email</label>
		<input id="email" type="email" autocomplete="email" />
		<button onclick="submitForgot()" id="btn-submit">Send reset link</button>
		<div class="error" id="error"></div>
		<div class="success" id="success"></div>
		<div class="link"><a href="{{.BasePath}}/app/login" id="link-back">Back to sign in</a></div>
	</div>
	<script>
		const basePath = '{{.BasePath}}';
		const i18n = {
			'en': { title: 'Forgot Password', email: 'Email', submit: 'Send reset link', back: 'Back to sign in', sent: 'If the email exists, a reset link has been sent.', failed: 'Request failed' },
			'zh-hans': { title: '忘记密码', email: '邮箱', submit: '发送重置链接', back: '返回登录', sent: '如果该邮箱存在，重置链接已发送。', failed: '请求失败' },
			'zh-hant': { title: '忘記密碼', email: '電子郵件', submit: '傳送重設連結', back: '返回登入', sent: '如果該電子郵件存在，重設連結已傳送。', failed: '請求失敗' }
		};
		function getLang() { return localStorage.getItem('lang') || 'en'; }
		function setLang(lang) { localStorage.setItem('lang', lang); applyLang(); }
		function changeLang() { setLang(document.getElementById('langSelect').value); }
		function applyLang() {
			const t = i18n[getLang()] || i18n['en'];
			document.getElementById('langSelect').value = getLang();
			document.getElementById('title').innerText = t.title;
			document.getElementById('lbl-email').innerText = t.email;
			document.getElementById('btn-submit').innerText = t.submit;
			document.getElementById('link-back').innerText = t.back;
		}
		applyLang();
		async function submitForgot() {
			const email = document.getElementById('email').value.trim();
			const t = i18n[getLang()] || i18n['en'];
			document.getElementById('error').innerText = '';
			document.getElementById('success').innerText = '';
			const res = await fetch(basePath + '/app/api/forgot-password', {
				method: 'POST', headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ email })
			});
			if (!res.ok) { document.getElementById('error').innerText = t.failed; return; }
			document.getElementById('success').innerText = t.sent;
		}
	</script>
</body>
</html>`))

var appResetPasswordTemplate = template.Must(template.New("appReset").Parse(`<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>NekoLc Reset Password</title>
	<style>` + sharedPageStyle + `</style>
</head>
<body>
	<div class="lang-switch">
		<select id="langSelect" onchange="changeLang()">
			<option value="en">English</option>
			<option value="zh-hans">简体中文</option>
			<option value="zh-hant">繁體中文</option>
		</select>
	</div>
	<div class="card">
		<h1 id="title">Reset Password</h1>
		<label id="lbl-password">New Password</label>
		<input id="password" type="password" autocomplete="new-password" />
		<label id="lbl-confirm">Confirm Password</label>
		<input id="confirmPassword" type="password" autocomplete="new-password" />
		<button onclick="submitReset()" id="btn-submit">Reset password</button>
		<div class="error" id="error"></div>
		<div class="success" id="success"></div>
		<div class="link"><a href="{{.BasePath}}/app/login" id="link-back">Back to sign in</a></div>
	</div>
	<script>
		const basePath = '{{.BasePath}}';
		const i18n = {
			'en': { title: 'Reset Password', password: 'New Password', confirm: 'Confirm Password', submit: 'Reset password', back: 'Back to sign in', mismatch: 'Passwords do not match', done: 'Password reset! Redirecting to login...', failed: 'Reset failed', notoken: 'Missing or invalid reset token' },
			'zh-hans': { title: '重置密码', password: '新密码', confirm: '确认密码', submit: '重置密码', back: '返回登录', mismatch: '两次密码不一致', done: '密码已重置！正在跳转到登录页面...', failed: '重置失败', notoken: '缺少或无效的重置令牌' },
			'zh-hant': { title: '重設密碼', password: '新密碼', confirm: '確認密碼', submit: '重設密碼', back: '返回登入', mismatch: '兩次密碼不一致', done: '密碼已重設！正在跳轉到登入頁面...', failed: '重設失敗', notoken: '缺少或無效的重設權杖' }
		};
		function getLang() { return localStorage.getItem('lang') || 'en'; }
		function setLang(lang) { localStorage.setItem('lang', lang); applyLang(); }
		function changeLang() { setLang(document.getElementById('langSelect').value); }
		function getToken() { return new URLSearchParams(window.location.search).get('token') || ''; }
		function applyLang() {
			const t = i18n[getLang()] || i18n['en'];
			document.getElementById('langSelect').value = getLang();
			document.getElementById('title').innerText = t.title;
			document.getElementById('lbl-password').innerText = t.password;
			document.getElementById('lbl-confirm').innerText = t.confirm;
			document.getElementById('btn-submit').innerText = t.submit;
			document.getElementById('link-back').innerText = t.back;
		}
		applyLang();
		async function submitReset() {
			const t = i18n[getLang()] || i18n['en'];
			const token = getToken();
			const password = document.getElementById('password').value;
			const confirmPassword = document.getElementById('confirmPassword').value;
			document.getElementById('error').innerText = '';
			document.getElementById('success').innerText = '';
			if (!token) { document.getElementById('error').innerText = t.notoken; return; }
			if (password !== confirmPassword) { document.getElementById('error').innerText = t.mismatch; return; }
			const res = await fetch(basePath + '/app/api/reset-password', {
				method: 'POST', headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ token, newPassword: password })
			});
			if (!res.ok) {
				const data = await res.json().catch(()=>({}));
				document.getElementById('error').innerText = (data.errors && data.errors[0] && data.errors[0].errorMessage) || t.failed;
				return;
			}
			document.getElementById('success').innerText = t.done;
			setTimeout(() => { window.location.href = basePath + '/app/login'; }, 1500);
		}
	</script>
</body>
</html>`))

var appVerifyEmailTemplate = template.Must(template.New("appVerify").Parse(`<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>NekoLc Verify Email</title>
	<style>` + sharedPageStyle + `</style>
</head>
<body>
	<div class="lang-switch">
		<select id="langSelect" onchange="changeLang()">
			<option value="en">English</option>
			<option value="zh-hans">简体中文</option>
			<option value="zh-hant">繁體中文</option>
		</select>
	</div>
	<div class="card">
		<h1 id="title">Verify Email</h1>
		<div class="success" id="success"></div>
		<div class="error" id="error"></div>
		<div class="link"><a href="{{.BasePath}}/app/login" id="link-back">Back to sign in</a></div>
	</div>
	<script>
		const basePath = '{{.BasePath}}';
		const i18n = {
			'en': { title: 'Verify Email', verifying: 'Verifying...', done: 'Your email has been verified.', failed: 'Verification failed or the link has expired.', back: 'Back to sign in', notoken: 'Missing verification token' },
			'zh-hans': { title: '验证邮箱', verifying: '正在验证...', done: '您的邮箱已验证。', failed: '验证失败或链接已过期。', back: '返回登录', notoken: '缺少验证令牌' },
			'zh-hant': { title: '驗證電子郵件', verifying: '正在驗證...', done: '您的電子郵件已驗證。', failed: '驗證失敗或連結已過期。', back: '返回登入', notoken: '缺少驗證權杖' }
		};
		function getLang() { return localStorage.getItem('lang') || 'en'; }
		function setLang(lang) { localStorage.setItem('lang', lang); applyLang(); }
		function changeLang() { setLang(document.getElementById('langSelect').value); }
		function getToken() { return new URLSearchParams(window.location.search).get('token') || ''; }
		function applyLang() {
			const t = i18n[getLang()] || i18n['en'];
			document.getElementById('langSelect').value = getLang();
			document.getElementById('title').innerText = t.title;
			document.getElementById('link-back').innerText = t.back;
		}
		async function verify() {
			const t = i18n[getLang()] || i18n['en'];
			const token = getToken();
			if (!token) { document.getElementById('error').innerText = t.notoken; return; }
			document.getElementById('success').innerText = t.verifying;
			const res = await fetch(basePath + '/app/api/verify-email?token=' + encodeURIComponent(token));
			if (!res.ok) {
				document.getElementById('success').innerText = '';
				document.getElementById('error').innerText = t.failed;
				return;
			}
			document.getElementById('error').innerText = '';
			document.getElementById('success').innerText = t.done;
		}
		applyLang();
		verify();
	</script>
</body>
</html>`))

var appAccountDisabledTemplate = template.Must(template.New("appDisabled").Parse(`<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>NekoLc</title>
	<style>` + sharedPageStyle + `
		.card { text-align: center; }
		.icon { font-size: 48px; margin-bottom: 8px; }
		.msg { color: #cbd5e1; margin-top: 8px; line-height: 1.6; }</style>
</head>
<body>
	<div class="lang-switch">
		<select id="langSelect" onchange="changeLang()">
			<option value="en">English</option>
			<option value="zh-hans">简体中文</option>
			<option value="zh-hant">繁體中文</option>
		</select>
	</div>
	<div class="card">
		<div class="icon">🔒</div>
		<h1 id="title">Account feature disabled</h1>
		<p class="msg" id="msg">The account feature is not enabled on this server.</p>
		<div class="link"><a href="{{.BasePath}}/app" id="link-home">Back to home</a></div>
	</div>
	<script>
		const i18n = {
			'en': { title: 'Account feature disabled', msg: 'The account feature is not enabled on this server. Please contact the administrator.', home: 'Back to home' },
			'zh-hans': { title: '账户功能未启用', msg: '此服务器未启用账户功能。请联系管理员。', home: '返回首页' },
			'zh-hant': { title: '帳戶功能未啟用', msg: '此伺服器未啟用帳戶功能。請聯絡管理員。', home: '返回首頁' }
		};
		function getLang() { return localStorage.getItem('lang') || 'en'; }
		function setLang(lang) { localStorage.setItem('lang', lang); applyLang(); }
		function changeLang() { setLang(document.getElementById('langSelect').value); }
		function applyLang() {
			const t = i18n[getLang()] || i18n['en'];
			document.getElementById('langSelect').value = getLang();
			document.getElementById('title').innerText = t.title;
			document.getElementById('msg').innerText = t.msg;
			document.getElementById('link-home').innerText = t.home;
		}
		applyLang();
	</script>
</body>
</html>`))

// accountModeEnabled reports whether the account/authentication feature is active.
func (s *Server) accountModeEnabled() bool {
	return s.authService != nil && s.authService.Enabled()
}

// renderIfAccountDisabled writes the "account feature disabled" page and returns
// true when the account feature is off, so callers can stop further handling.
func (s *Server) renderIfAccountDisabled(w http.ResponseWriter) bool {
	if s.accountModeEnabled() {
		return false
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	appAccountDisabledTemplate.Execute(w, map[string]interface{}{"BasePath": s.basePath})
	return true
}

func (s *Server) handleAppForgotPasswordPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	appForgotPasswordTemplate.Execute(w, map[string]interface{}{"BasePath": s.basePath})
}

func (s *Server) handleAppResetPasswordPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	appResetPasswordTemplate.Execute(w, map[string]interface{}{"BasePath": s.basePath})
}

func (s *Server) handleAppVerifyEmailPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	appVerifyEmailTemplate.Execute(w, map[string]interface{}{"BasePath": s.basePath})
}
