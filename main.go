package main

import (
	"crypto/tls"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// メール送信設定 (環境変数から読み込む構造体)
type EmailConfig struct {
	SMTPServer string
	SMTPPort   string
	Sender     string
	Password   string
}

func main() {
	// 1. 静的ファイル配信（CSS の場合は Content-Type を明示）
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Static request: %s", r.URL.Path)
		if strings.HasSuffix(r.URL.Path, ".css") {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		}
		fs.ServeHTTP(w, r)
	})))

	// 2. トップページ
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseGlob("templates/*.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tmpl.ExecuteTemplate(w, "index.html", nil)
	})

	// 3. お問い合わせ送信ハンドラ
	http.HandleFunc("/contact", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// フォーム値の取得
		name := r.FormValue("name")
		email := r.FormValue("email")
		message := r.FormValue("message")
		log.Printf("Contact POST received: name=%s email=%s from %s", name, email, r.RemoteAddr)

		// --- メール送信処理 ---
		err := sendEmail(name, email, message)
		if err != nil {
			log.Printf("メール送信エラー: %v", err)
			// エラー時はエラーメッセージを返す（HTMXで表示するため）
			http.Error(w, "メール送信に失敗しました。時間をおいて再度お試しください。", http.StatusInternalServerError)
			return
		}

		log.Printf("送信成功: %s <%s>", name, email)

		// 送信完了画面 (HTMXレスポンス)
		successHTML := fmt.Sprintf(`
			<div class="bg-green-100 border border-green-400 text-green-700 px-4 py-10 rounded relative text-center">
				<strong class="font-bold text-xl block mb-2">送信完了！</strong>
				<span class="block sm:inline">%s 様、お問い合わせありがとうございます。</span>
				<br>
				<span class="text-sm mt-4 block">確認メールを %s 宛に送信しました。</span>
				
				<button onclick="location.reload()" class="mt-6 bg-green-600 hover:bg-green-700 text-white font-bold py-2 px-4 rounded transition duration-200">
					戻る
				</button>
			</div>
		`, name, email)

		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(successHTML))
	})

	// Debug endpoint — 一時的 (パスワード等は表示しません)
	http.HandleFunc("/_debug/smtp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		server := os.Getenv("SMTP_SERVER")
		portEnv := os.Getenv("SMTP_PORT")
		sender := os.Getenv("SMTP_EMAIL")
		fmt.Fprintf(w, "SMTP_SERVER=%q\nSMTP_PORT=%q\nSMTP_EMAIL=%q\n\n", server, portEnv, sender)

		if server == "" || portEnv == "" {
			fmt.Fprintln(w, "SMTP_SERVER or SMTP_PORT not set")
			return
		}

		// DNS lookup
		ips, err := net.LookupHost(server)
		if err != nil {
			fmt.Fprintf(w, "DNS lookup failed: %v\n", err)
		} else {
			fmt.Fprintf(w, "Resolved IPs: %v\n", ips)
		}

		// Try TCP dial
		addr := net.JoinHostPort(server, portEnv)
		d := net.Dialer{Timeout: 5 * time.Second}
		conn, err := d.Dial("tcp", addr)
		if err != nil {
			fmt.Fprintf(w, "Dial TCP %s error: %v\n", addr, err)
			return
		}
		conn.Close()
		fmt.Fprintf(w, "Dial TCP %s: success\n", addr)
	})

	// Render対応のポート設定
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server running on http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// メール送信のヘルパー関数
func sendEmail(name, userEmail, messageBody string) error {
	// 環境変数から設定を取得
	config := EmailConfig{
		SMTPServer: os.Getenv("SMTP_SERVER"),   // 例: smtp.gmail.com
		SMTPPort:   os.Getenv("SMTP_PORT"),     // 例: 587
		Sender:     os.Getenv("SMTP_EMAIL"),    // 送信元メールアドレス
		Password:   os.Getenv("SMTP_PASSWORD"), // アプリパスワード
	}

	// 設定が足りない場合はログを出してスキップ (ローカル開発用)
	if config.Sender == "" || config.Password == "" {
		log.Println("⚠️ SMTP設定が見つかりません。メール送信をスキップします。")
		return nil
	}

	// 認証設定
	auth := smtp.PlainAuth("", config.Sender, config.Password, config.SMTPServer)

	// メールの内容作成
	// Subject: 件名
	// From: 送信元
	// To: 送信先 (今回は管理者宛とユーザー宛を兼ねて自分に送る、あるいはユーザーに送る)
	// ※ここでは「管理者(自分)に通知」しつつ、「ユーザー」にはCCで送る例にします
	to := []string{config.Sender, userEmail}

	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: 【お問い合わせ】%s 様より\r\n"+
		"\r\n"+
		"以下の内容でお問い合わせを受け付けました。\r\n"+
		"--------------------------------\r\n"+
		"名前: %s\r\n"+
		"Email: %s\r\n"+
		"\r\n"+
		"本文:\r\n%s\r\n"+
		"--------------------------------\r\n",
		userEmail, name, name, userEmail, messageBody))

	// 送信は非同期で行う（同期だと接続できないSMTP宛先でリクエストがハングするため）
	go func() {
		addr := config.SMTPServer + ":" + config.SMTPPort
		d := net.Dialer{Timeout: 10 * time.Second}
		conn, err := d.Dial("tcp", addr)
		if err != nil {
			log.Printf("SMTP dial error: %v", err)
			return
		}
		// TLS / STARTTLS handling and SMTP client
		client, err := smtp.NewClient(conn, config.SMTPServer)
		if err != nil {
			log.Printf("SMTP client error: %v", err)
			return
		}
		defer client.Close()

		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsConfig := &tls.Config{ServerName: config.SMTPServer}
			if err := client.StartTLS(tlsConfig); err != nil {
				log.Printf("SMTP STARTTLS error: %v", err)
				return
			}
		}

		if err := client.Auth(auth); err != nil {
			log.Printf("SMTP auth error: %v", err)
			return
		}

		if err := client.Mail(config.Sender); err != nil {
			log.Printf("SMTP MAIL FROM error: %v", err)
			return
		}
		for _, rcpt := range to {
			if err := client.Rcpt(rcpt); err != nil {
				log.Printf("SMTP RCPT error for %s: %v", rcpt, err)
				return
			}
		}

		w, err := client.Data()
		if err != nil {
			log.Printf("SMTP DATA error: %v", err)
			return
		}

		if _, err := w.Write(msg); err != nil {
			log.Printf("SMTP write error: %v", err)
			return
		}

		if err := w.Close(); err != nil {
			log.Printf("SMTP DATA close error: %v", err)
			return
		}

		if err := client.Quit(); err != nil {
			log.Printf("SMTP QUIT error: %v", err)
		}

		log.Printf("SMTP send finished (async) to %v", to)
	}()

	return nil
}
