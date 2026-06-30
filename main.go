package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/SevereCloud/vksdk/v2/api"
	"github.com/SevereCloud/vksdk/v2/api/params"
	"github.com/SevereCloud/vksdk/v2/events"
	"github.com/SevereCloud/vksdk/v2/longpoll-bot"
	"github.com/go-co-op/gocron"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Task struct {
	ID        uint   `gorm:"primaryKey"`
	UserID    int    `gorm:"index"`
	Text      string `gorm:"not null"`
	RemindAt  time.Time
	IsDone    bool `gorm:"default:false"`
	CreatedAt time.Time
}

var db *gorm.DB
var vk *api.VK

func main() {
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
		os.Getenv("DB_PORT"),
	)
	var err error
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}
	db.AutoMigrate(&Task{})

	token := os.Getenv("VK_TOKEN")
	if token == "" {
		log.Fatal("VK_TOKEN не задан")
	}
	vk = api.NewVK(token)

	group, err := vk.GroupsGetByID(nil)
	if err != nil {
		log.Fatal("Ошибка получения данных группы:", err)
	}

	s := gocron.NewScheduler(time.Local)
	s.Every(1).Minute().Do(checkReminders)
	s.StartAsync()

	lp, err := longpoll.NewLongPoll(vk, group[0].ID)
	if err != nil {
		log.Fatal("Ошибка LongPoll:", err)
	}

	fmt.Println("Бот запущен и готов к работе...")

	lp.MessageNew(func(ctx context.Context, obj events.MessageNewObject) {
		text := obj.Message.Text
		userID := obj.Message.PeerID
		log.Printf("[%d] прислал: %s", userID, text)

		if strings.HasPrefix(text, "/add") {
			handleAddTask(userID, text)
		} else if text == "/list" {
			handleListTasks(userID)
		} else {
			sendMessage(userID, "Доступные команды:\n/add [текст] ГГГГ-ММ-ДД ЧЧ:ММ\n/list")
		}
	})

	lp.Run()
}

func handleAddTask(userID int, fullText string) {
	parts := strings.Split(fullText, " ")
	if len(parts) < 4 {
		sendMessage(userID, "Используйте: /add [текст] ГГГГ-ММ-ДД ЧЧ:ММ")
		return
	}
	timePart := parts[len(parts)-2] + " " + parts[len(parts)-1]
	taskText := strings.Join(parts[1:len(parts)-2], " ")
	remindTime, err := time.Parse("2006-01-02 15:04", timePart)
	if err != nil {
		sendMessage(userID, "Ошибка формата даты!")
		return
	}
	newTask := Task{UserID: userID, Text: taskText, RemindAt: remindTime}
	if err := db.Create(&newTask).Error; err != nil {
		sendMessage(userID, "Ошибка сохранения.")
		return
	}
	sendMessage(userID, fmt.Sprintf("Задача добавлена! Напомню: %s", remindTime.Format("02.01.2006 в 15:04")))
}

func handleListTasks(userID int) {
	var tasks []Task
	db.Where("user_id = ? AND is_done = ?", userID, false).Order("remind_at asc").Find(&tasks)
	if len(tasks) == 0 {
		sendMessage(userID, "Нет активных задач.")
		return
	}
	var sb strings.Builder
	sb.WriteString("Ваши задачи:\n")
	for i, t := range tasks {
		sb.WriteString(fmt.Sprintf("%d. %s — %s\n", i+1, t.Text, t.RemindAt.Format("02.01 15:04")))
	}
	sendMessage(userID, sb.String())
}

func checkReminders() {
	var tasks []Task
	db.Where("remind_at <= ? AND is_done = ?", time.Now(), false).Find(&tasks)
	for _, t := range tasks {
		sendMessage(t.UserID, fmt.Sprintf("Напоминание: %s", t.Text))
		db.Model(&t).Update("is_done", true)
	}
}

func sendMessage(peerID int, text string) {
	b := params.NewMessagesSendBuilder()
	b.PeerID(peerID)
	b.RandomID(0)
	b.Message(text)
	_, err := vk.MessagesSend(b.Params)
	if err != nil {
		log.Printf("Ошибка отправки: %v", err)
	}
}
