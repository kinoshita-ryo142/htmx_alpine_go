package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
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

	// 送信実行
	err := smtp.SendMail(
		config.SMTPServer+":"+config.SMTPPort,
		auth,
		config.Sender,
		to,
		msg,
	)

	return err
}
