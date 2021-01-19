// Copyright (c) 2015 Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package server

import (
	"os"
	"time"
	"strings"
	"bytes"
	"io/ioutil"
	"strconv"
	"encoding/json"
	"net/http"
	"path/filepath"
)

var WechatAccessToken string
var WechatExpiresTime time.Time

type AndroidNotificationServerW struct {
	AndroidPushSettings AndroidPushSettings
	metrics             *metrics
	logger              *Logger
}

type KeyWordData struct {
	Value string `json:"value"`
	Color string `json:"color,omitempty"`
}

type TemplateMsg struct {
	Touser      string                  `json:"touser"`
	Template_id string                  `json:"template_id"`
	Url         string                  `json:"url,omitempty"`
	Data        map[string]*KeyWordData `json:"data"`
}

type WPushResponse struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func NewAndroidNotificationServerW(settings AndroidPushSettings, logger *Logger, metrics *metrics) NotificationServer {
	return &AndroidNotificationServerW{
		AndroidPushSettings: settings,
		metrics:             metrics,
		logger:              logger,
	}
}

func (me *AndroidNotificationServerW) Initialize() bool {
	me.logger.Infof("Initializing Android notification server for type=%v", me.AndroidPushSettings.Type)

	if me.AndroidPushSettings.AndroidAPIKey == "" {
		me.logger.Error("Android push notifications not configured.  Missing AndroidAPIKey.")
		return false
	}

	return true
}

func (me *AndroidNotificationServerW) SendNotification(msg *PushNotification) PushResponse {
	pushType := msg.Type
	var data map[string]string
	if _, err := os.Stat("./config/wechat-device-ids.json"); err != nil {
		return NewErrorPushResponse("Map not found error")
	}
	fileName, _ := filepath.Abs("./config/wechat-device-ids.json")
	mapData, _ := ioutil.ReadFile(fileName)
	err := json.Unmarshal(mapData, &data)
	if err != nil {
		return NewErrorPushResponse("Unmarshal map error")
	}
	deviceId, exists := data[msg.DeviceID]
	if !exists {
		return NewErrorPushResponse("No map error")
	}

	if me.metrics != nil {
		me.metrics.incrementNotificationTotal(PushNotifyAndroid, pushType)
	}

	dataMsg := make(map[string]*KeyWordData)
	dataMsg["content"] = &KeyWordData{
		Value: msg.SenderName + ": " + msg.Message,
	}
	message := &TemplateMsg{
		Touser:      deviceId,
		Template_id: "3qW96y74I5Wari8oFvmu82fj9yS4LNyfPrmtLadydrI",
		Data:        dataMsg,
	}
	body, _ := json.MarshalIndent(message, " ", "  ")
	if me.AndroidPushSettings.AndroidAPIKey != "" {
		token, err := GetToken(me.AndroidPushSettings.AndroidAPIKey)
		me.logger.Errorf("Token=%v", token)
		if err != nil {
			me.logger.Errorf("Failed to get Wechat token sid=%v did=%v err=%v type=%v", msg.ServerID, msg.DeviceID, err, me.AndroidPushSettings.Type)
			return NewErrorPushResponse("Getting token error")
		}
		post_url := "https://api.weixin.qq.com/cgi-bin/message/template/send?access_token=" + token
		req, _ := http.NewRequest("POST", post_url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json;encoding=utf-8")
		client := &http.Client{}

		me.logger.Infof("Sending android push notification for device=%v and type=%v", me.AndroidPushSettings.Type, msg.Type)

		start := time.Now()
		if me.metrics != nil {
			me.metrics.observerNotificationResponse(PushNotifyAndroid, time.Since(start).Seconds())
		}

		resp, err := client.Do(req)
		if err != nil {
			if resp.Body != nil {
				resp.Body.Close()
			}
			me.logger.Errorf("Failed to send Wechat push sid=%v did=%v err=%v type=%v", msg.ServerID, msg.DeviceID, err, me.AndroidPushSettings.Type)
			if me.metrics != nil {
				me.metrics.incrementFailure(PushNotifyAndroid, pushType, "unknown transport error")
			}
			return NewErrorPushResponse("unknown transport error")
		}

		defer resp.Body.Close()
		var response WPushResponse
		respBody, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			me.logger.Errorf("Failed to read response: %v sid=%v did=%v err=%v type=%v", resp, msg.ServerID, msg.DeviceID, err, me.AndroidPushSettings.Type)
			if me.metrics != nil {
				me.metrics.incrementFailure(PushNotifyAndroid, pushType, "invalid response")
			}
			return NewErrorPushResponse("invalid response")
		}

		err = json.Unmarshal(respBody, &response)
		if err != nil {
			me.logger.Errorf("Failed to unmarshal response: %v sid=%v did=%v err=%v type=%v", resp, msg.ServerID, msg.DeviceID, err, me.AndroidPushSettings.Type)
			if me.metrics != nil {
				me.metrics.incrementFailure(PushNotifyAndroid, pushType, "invalid response")
			}
			return NewErrorPushResponse("invalid response")
		}

		if response.ErrCode != 0 {
			me.logger.Errorf("Failed to send Wechat push sid=%v did=%v err=%v type=%v", msg.ServerID, msg.DeviceID, response.ErrCode, me.AndroidPushSettings.Type)
			if me.metrics != nil {
				me.metrics.incrementFailure(PushNotifyAndroid, pushType, response.ErrMsg)
			}
			return NewErrorPushResponse(response.ErrMsg)
		}
		me.logger.Errorf("Sent resp=%v", resp)
	}

	if me.metrics != nil {
		if msg.AckID != "" {
			me.metrics.incrementSuccessWithAck(PushNotifyAndroid, pushType)
		} else {
			me.metrics.incrementSuccess(PushNotifyAndroid, pushType)
		}
	}
	return NewOkPushResponse()
}


func GetToken(key string) (string, error) {
	if time.Now().Before(WechatExpiresTime) {
		return WechatAccessToken, nil
	}
	idSec := strings.Split(key, ":")
	url := "https://api.weixin.qq.com/cgi-bin/token?grant_type=client_credential&appid=" + idSec[0] + "&secret=" + idSec[1]
	resp, err := http.Get(url)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return "", err
	}
	if resp == nil {
		return "", nil
	}

	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var response TokenResponse
	err = json.Unmarshal(respBody, &response)
	if err != nil {
		return "", err
	}
	WechatAccessToken = response.AccessToken
	s, _ := time.ParseDuration(strconv.Itoa(response.ExpiresIn) + "s")
	WechatExpiresTime = time.Now().Add(s)
	return WechatAccessToken, nil
}