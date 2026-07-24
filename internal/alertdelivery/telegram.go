package alertdelivery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	telegramAPIBase       = "https://api.telegram.org"
	maxTelegramTextBytes  = 4096
	maxTelegramReplyBytes = 64 * 1024
)

type telegramResponse struct {
	OK bool `json:"ok"`
}

type TelegramClient struct {
	client *http.Client
	url    string
	chatID string
}

func NewTelegramClient(
	client *http.Client,
	botToken string,
	chatID string,
) (*TelegramClient, error) {
	if client == nil ||
		client.Timeout < time.Second ||
		client.Timeout > 30*time.Second ||
		!validBotToken(botToken) ||
		!validChatID(chatID) {
		return nil, ErrInvalidDelivery
	}
	boundedClient := *client
	boundedClient.CheckRedirect = func(
		_ *http.Request,
		_ []*http.Request,
	) error {
		return http.ErrUseLastResponse
	}
	return &TelegramClient{
		client: &boundedClient,
		url:    telegramAPIBase + "/bot" + botToken + "/sendMessage",
		chatID: chatID,
	}, nil
}

func (client *TelegramClient) Send(ctx context.Context, text string) error {
	if client == nil ||
		client.client == nil ||
		ctx == nil ||
		text == "" ||
		len(text) > maxTelegramTextBytes {
		return ErrTelegramUnavailable
	}
	body, err := json.Marshal(struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}{
		ChatID: client.chatID,
		Text:   text,
	})
	if err != nil {
		return ErrTelegramUnavailable
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.url,
		bytes.NewReader(body),
	)
	if err != nil {
		return ErrTelegramUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.client.Do(request)
	if err != nil {
		return ErrTelegramUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxTelegramReplyBytes))
		return ErrTelegramUnavailable
	}
	var result telegramResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxTelegramReplyBytes))
	if err := decoder.Decode(&result); err != nil || !result.OK {
		return ErrTelegramUnavailable
	}
	return nil
}

func validBotToken(value string) bool {
	if len(value) < 24 || len(value) > 256 || strings.Count(value, ":") != 1 {
		return false
	}
	parts := strings.SplitN(value, ":", 2)
	if len(parts[0]) == 0 || len(parts[1]) < 16 {
		return false
	}
	for _, character := range parts[0] {
		if character < '0' || character > '9' {
			return false
		}
	}
	for _, character := range parts[1] {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_' ||
			character == '-' {
			continue
		}
		return false
	}
	return true
}

func validChatID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	if value[0] == '@' {
		if len(value) < 6 {
			return false
		}
		for _, character := range value[1:] {
			if (character >= 'a' && character <= 'z') ||
				(character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') ||
				character == '_' {
				continue
			}
			return false
		}
		return true
	}
	start := 0
	if value[0] == '-' {
		start = 1
	}
	if start == len(value) {
		return false
	}
	for _, character := range value[start:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
