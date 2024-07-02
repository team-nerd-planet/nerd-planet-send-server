package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/smtp"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/slack-go/slack"
	"github.com/spf13/viper"
	"github.com/team-nerd-planet/send-server/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	conf, err := NewConfig()
	if err != nil {
		panic(err)
	}

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable TimeZone=Asia/Seoul",
		conf.Database.Host,
		conf.Database.Port,
		conf.Database.UserName,
		conf.Database.Password,
		conf.Database.DbName,
	)

	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold:             time.Second,        // Slow SQL threshold
			LogLevel:                  logger.LogLevel(4), // Log level
			IgnoreRecordNotFoundError: true,               // Ignore ErrRecordNotFound error for logger
			ParameterizedQueries:      false,              // Don't include params in the SQL log
			Colorful:                  false,              // Disable color
		},
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: newLogger,
	})
	if err != nil {
		panic(err)
	}

	// location, err := time.LoadLocation("Asia/Seoul")
	// if err != nil {
	// 	slog.Error("Unfortunately can't load a location", "error", err.Error())
	// } else {
	// 	time.Local = location
	// 	gocron.ChangeLoc(location)
	// }

	// slog.Info("current time", "time", time.Now())

	// gocron.Every(1).Day().At("07:00").Do(func() {
	// 	slog.Info("start schedule", "time", *location)

	var subscriptionArr []entity.Subscription

	if err := db.Find(&subscriptionArr).Error; err != nil {
		panic(err)
	}

	var sb strings.Builder
	sb.WriteString("안녕하세요! plaa 입니다.\n너드플라넷의 소식을 아래와 같이 전달했어요!!\n\n")

	res := make(chan PublishResult, len(subscriptionArr))

	fmt.Printf("subscriptionArr = %d\n", len(subscriptionArr))

	for _, subscription := range subscriptionArr {
		go publish(res, conf, db, subscription)
	}

	var v PublishResult
	var ok bool

	for v, ok = <-res; ok; v, ok = <-res {
		fmt.Println("RES!!!!!!")
		sb.WriteString(fmt.Sprintf("%s님에게 %d개의 리스트를 보냈어요.\n", v.name, v.count))
	}

	// for r := range res {
	// 	fmt.Println("RES!!!!!!")
	// 	sb.WriteString(fmt.Sprintf("%s님에게 %d개의 리스트를 보냈어요.\n", r.name, r.count))
	// }

	// if err := webhook(conf, sb.String()); err != nil {
	// 	panic(err)
	// }
	// })

	// <-gocron.Start()
}

type PublishResult struct {
	name  string
	count int
}

func publish(res chan PublishResult, conf *Config, db *gorm.DB, subscription entity.Subscription) {
	var (
		items []entity.ItemView
		where = make([]string, 0)
		param = make([]interface{}, 0)
		name  string
		count int = 0
	)

	if subscription.Name != nil {
		name = *subscription.Name
	} else {
		name = strings.Split(subscription.Email, "@")[0]
	}

	defer func() {
		res <- PublishResult{name: name, count: count}
	}()

	where = append(where, "? <= item_published")
	param = append(param, subscription.Published)

	// TODO: 카테고라이징이 제대로 준비될 때까지 임시로 조건 비활성화
	// if len(subscription.PreferredCompanyArr) > 0 {
	// 	where = append(where, "feed_id IN ?")
	// 	param = append(param, []int64(subscription.PreferredCompanyArr))
	// }

	// if len(subscription.PreferredCompanySizeArr) > 0 {
	// 	where = append(where, "company_size IN ?")
	// 	param = append(param, []int64(subscription.PreferredCompanySizeArr))
	// }

	// if len(subscription.PreferredJobArr) > 0 {
	// 	where = append(where, "job_tags_id_arr && ?") // `&&`: overlap (have elements in common)
	// 	param = append(param, getArrToString(subscription.PreferredJobArr))
	// }

	// if len(subscription.PreferredSkillArr) > 0 {
	// 	where = append(where, "skill_tags_id_arr && ?") // `&&`: overlap (have elements in common)
	// 	param = append(param, getArrToString(subscription.PreferredSkillArr))
	// }

	if err := db.Select(
		"item_title",
		"LEFT(item_description, 50) AS item_description",
		"item_link",
		"CASE WHEN item_thumbnail = '' OR item_thumbnail IS NULL THEN 'https://www.nerdplanet.app/images/feed-thumbnail.png' ELSE item_thumbnail END AS item_thumbnail",
		"feed_name",
	).Where(strings.Join(where, " AND "), param...).Limit(5).Find(&items).Error; err != nil {
		slog.Error(err.Error(), "error", err)
		return
	}

	count = len(items)

	if count > 0 {
		data := struct {
			Name   string
			Length int
			Items  []entity.ItemView
		}{
			Name:   name,
			Length: count,
			Items:  items,
		}

		_, b, _, _ := runtime.Caller(0)
		configDirPath := path.Join(path.Dir(b))
		t, err := template.ParseFiles(fmt.Sprintf("%s/template/newsletter.html", configDirPath))
		if err != nil {
			slog.Error(err.Error(), "error", err)
			return
		}

		var body bytes.Buffer
		if err := t.Execute(&body, data); err != nil {
			slog.Error(err.Error(), "error", err)
			return
		}

		auth := smtp.PlainAuth("", conf.Smtp.UserName, conf.Smtp.Password, conf.Smtp.Host)
		from := conf.Smtp.UserName
		to := []string{subscription.Email}
		subject := "Subject: 너드플래닛 기술블로그 뉴스레터 \n"
		mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
		msg := []byte(subject + mime + body.String())
		err = smtp.SendMail(fmt.Sprintf("%s:%d", conf.Smtp.Host, conf.Smtp.Port), auth, from, to, msg)
		if err != nil {
			slog.Error(err.Error(), "error", err)
			return
		}
	}

	// subscription.Published = time.Now()
	// if err := db.Save(subscription).Error; err != nil {
	// 	slog.Error(err.Error(), "error", err)
	// 	return
	// }
}

func getArrToString(arr []int64) string {
	strArr := make([]string, len(arr))
	for i, v := range arr {
		strArr[i] = strconv.FormatInt(v, 10)
	}

	return fmt.Sprintf("{%s}", strings.Join(strArr, ","))
}

type Config struct {
	Database Database `mapstructure:"DATABASE"`
	Smtp     Smtp     `mapstructure:"SMTP"`
	Webhook  Webhook  `mapstructure:"WEBHOOK"`
}

type Database struct {
	Host     string `mapstructure:"HOST"`
	Port     int    `mapstructure:"PORT"`
	LogLevel int    `mapstructure:"LOG_LEVEL"` // 1:Silent, 2:Error, 3:Warn, 4:Info
	UserName string `mapstructure:"USER_NAME"`
	Password string `mapstructure:"PASSWORD"`
	DbName   string `mapstructure:"DB_NAME"`
}

type Smtp struct {
	Host     string `mapstructure:"HOST"`
	Port     int    `mapstructure:"PORT"`
	UserName string `mapstructure:"USER_NAME"`
	Password string `mapstructure:"PASSWORD"`
}

type Webhook struct {
	Key string `mapstructure:"KEY"`
}

func NewConfig() (*Config, error) {
	_, b, _, _ := runtime.Caller(0)
	configDirPath := path.Join(path.Dir(b))

	conf := Config{}
	viper.SetConfigName("config")
	viper.SetConfigType("json")
	viper.AddConfigPath(configDirPath)

	err := viper.ReadInConfig()
	if err != nil {
		slog.Error("Read config file.", "err", err)
		return nil, err
	}

	viper.AutomaticEnv()

	err = viper.Unmarshal(&conf)
	if err != nil {
		slog.Error("Unmarshal config file.", "err", err)
		return nil, err
	}

	return &conf, nil
}

func webhook(conf *Config, text string) error {
	attachment := slack.Attachment{
		Color:         "good",
		Fallback:      "You successfully posted by Incoming Webhook URL!",
		AuthorName:    "plaa",
		AuthorSubname: "I live in Nerd Planet.",
		AuthorLink:    "https://www.nerdplanet.app",
		AuthorIcon:    "https://avatars.slack-edge.com/2024-06-08/7245446528738_95ffe7a911c7aced7f3c_512.png",
		Text:          text,
		Footer:        "plaa",
		FooterIcon:    "https://avatars.slack-edge.com/2024-06-08/7245446528738_95ffe7a911c7aced7f3c_512.png",
		Ts:            json.Number(strconv.FormatInt(time.Now().Unix(), 10)),
	}
	msg := slack.WebhookMessage{
		Attachments: []slack.Attachment{attachment},
	}

	return slack.PostWebhook(conf.Webhook.Key, &msg)
}
