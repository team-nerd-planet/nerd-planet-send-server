package main

import (
	"bytes"
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

	"github.com/jasonlvhit/gocron"
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

	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		slog.Error("Unfortunately can't load a location", "error", err.Error())
	} else {
		gocron.ChangeLoc(location)
	}

	gocron.Every(1).Day().At("09:10").Do(func() {
		slog.Info("start cron", "time", *location)

		var subscriptionArr []entity.Subscription

		if err := db.Find(&subscriptionArr).Error; err != nil {
			panic(err)
		}

		for _, subscription := range subscriptionArr {
			publish(conf, db, subscription)
		}
	})

	<-gocron.Start()
}

func publish(conf *Config, db *gorm.DB, subscription entity.Subscription) {
	var (
		items []entity.ItemView
		where = make([]string, 0)
		param = make([]interface{}, 0)
	)

	where = append(where, "? <= item_published")
	param = append(param, subscription.Published)

	if len(subscription.PreferredCompanyArr) > 0 {
		where = append(where, "feed_id IN ?")
		param = append(param, []int64(subscription.PreferredCompanyArr))
	}

	if len(subscription.PreferredCompanySizeArr) > 0 {
		where = append(where, "company_size IN ?")
		param = append(param, []int64(subscription.PreferredCompanySizeArr))
	}

	if len(subscription.PreferredJobArr) > 0 {
		where = append(where, "job_tags_id_arr && ?") // `&&`: overlap (have elements in common)
		param = append(param, getArrToString(subscription.PreferredJobArr))
	}

	if len(subscription.PreferredSkillArr) > 0 {
		where = append(where, "skill_tags_id_arr && ?") // `&&`: overlap (have elements in common)
		param = append(param, getArrToString(subscription.PreferredSkillArr))
	}

	if err := db.Select(
		"item_title",
		"LEFT(item_description, 50) as item_description",
		"item_link",
		"NULLIF(item_thumbnail, 'https://www.nerdplanet.app/images/feed-thumbnail.png') as item_thumbnail",
		"feed_name",
	).Where(strings.Join(where, " AND "), param...).Limit(10).Find(&items).Error; err != nil {
		slog.Error(err.Error(), "error", err)
		return
	}

	if len(items) > 0 {
		_, b, _, _ := runtime.Caller(0)
		configDirPath := path.Join(path.Dir(b))
		t, err := template.ParseFiles(fmt.Sprintf("%s/template/newsletter.html", configDirPath))
		if err != nil {
			slog.Error(err.Error(), "error", err)
			return
		}

		var body bytes.Buffer
		if err := t.Execute(&body, items); err != nil {
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

	subscription.Published = time.Now()
	if err := db.Save(subscription).Error; err != nil {
		slog.Error(err.Error(), "error", err)
		return
	}
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
