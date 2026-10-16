package core

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
)

type startupTransportFunc func(*http.Request) (*http.Response, error)

func (f startupTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestStartupLogsDoNotExposeBotToken(t *testing.T) {
	const token = "123456:synthetic-startup-canary"
	logger := log.StandardLogger()
	previousConfig := msbconf
	previousTransport := http.DefaultTransport
	previousOutput, previousFormatter := logger.Out, logger.Formatter
	previousLevel, previousExit := logger.GetLevel(), logger.ExitFunc
	t.Cleanup(func() {
		msbconf = previousConfig
		http.DefaultTransport = previousTransport
		logger.SetOutput(previousOutput)
		logger.SetFormatter(previousFormatter)
		logger.SetLevel(previousLevel)
		logger.ExitFunc = previousExit
	})

	for _, level := range log.AllLevels {
		for _, fails := range []bool{false, true} {
			name := level.String() + "/success"
			if fails {
				name = level.String() + "/transport-failure"
			}
			t.Run(name, func(t *testing.T) {
				var output bytes.Buffer
				var exits []int
				requests := 0
				msbconf = ConfigTemplate{BotToken: token, LogLevel: level.String()}
				logger.SetOutput(&output)
				logger.SetFormatter(&log.TextFormatter{DisableTimestamp: true, DisableColors: true})
				logger.SetLevel(level)
				logger.ExitFunc = func(code int) { exits = append(exits, code) }
				http.DefaultTransport = startupTransportFunc(func(req *http.Request) (*http.Response, error) {
					requests++
					if req.URL.Path != "/bot"+token+"/getMe" {
						t.Error("startup request did not preserve the token and getMe method")
					}
					if fails {
						return nil, errors.New("synthetic transport failure")
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":{"id":42,"is_bot":true,"first_name":"Test","username":"test_bot"}}`)),
					}, nil
				})

				bot := initBot(msbconf)
				if requests != 1 {
					t.Fatalf("expected one intercepted startup request, got %d", requests)
				}
				if strings.Contains(output.String(), token) {
					t.Error("startup logs contain the bot token")
				}
				if fails {
					if bot != nil || len(exits) != 1 || exits[0] != 1 {
						t.Fatal("failed initialization must return no bot and request exit code 1")
					}
					if level >= log.FatalLevel && !strings.Contains(output.String(), "synthetic transport failure") {
						t.Error("startup failure lost its diagnostic")
					}
				} else {
					if bot == nil || bot.Me == nil || bot.Me.ID != 42 || bot.Token != token || len(exits) != 0 {
						t.Fatal("successful initialization did not preserve the bot identity and credential")
					}
					if level >= log.InfoLevel && !strings.Contains(output.String(), "Attempting to initialize...") {
						t.Error("startup progress message is missing")
					}
				}
			})
		}
	}
}
