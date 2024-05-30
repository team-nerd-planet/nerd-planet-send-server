package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/team-nerd-planet/send-server/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable TimeZone=Asia/Seoul",
		"localhost",
		5432,
		"nerd",
		"planet1!",
		"nerd_planet",
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

	var subscriptionArr []entity.Subscription

	if err := db.Find(&subscriptionArr).Error; err != nil {
		panic(err)
	}

	slog.Info("Find subscription", "subscription", subscriptionArr)

	for _, subscription := range subscriptionArr {
		var (
			items []entity.ItemView
			where = make([]string, 0)
			param = make([]interface{}, 0)
		)

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

		if err := db.Where(strings.Join(where, " AND "), param...).Find(&items).Limit(10).Error; err != nil {
			panic(err)
		}

		if len(items) > 0 {
			
			// 해당 메일 전송
		}
		//https://www.nerdplanet.app/images/feed-thumbnail.png
		subscription.Published = time.Now()
		if err := db.Save(subscription).Error; err != nil {
			panic(err)
		}
	}

	// 모든 구독자 조회
	// 어제(YYYYMMDD) 업데이트 글 중에 해당 구독자가 선호하는 글 목록 최대 10개 조회
	// -> 해당 글이 있으면
	//    해당 메일로 전송
	// 해당 구독자 Published 값 갱신
}

func getArrToString(arr []int64) string {
	strArr := make([]string, len(arr))
	for i, v := range arr {
		strArr[i] = strconv.FormatInt(v, 10)
	}

	return fmt.Sprintf("{%s}", strings.Join(strArr, ","))
}
