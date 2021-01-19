// Copyright (c) 2015 Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package server

import (
	"time"
	"strings"
	"bytes"
	"io/ioutil"
	"encoding/json"
	"net/http"
	"github.com/kyokomi/emoji"
)

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
	data := map[string]interface{}{
		"ack_id":     msg.AckID,
		"type":       pushType,
		"badge":      msg.Badge,
		"version":    msg.Version,
		"channel_id": msg.ChannelID,
	}

	if msg.IsIDLoaded {
		data["post_id"] = msg.PostID
		data["message"] = msg.Message
		data["id_loaded"] = true
		data["sender_id"] = msg.SenderID
		data["sender_name"] = "Someone"
	} else if pushType == PushTypeMessage || pushType == PushTypeSession {
		data["team_id"] = msg.TeamID
		data["sender_id"] = msg.SenderID
		data["sender_name"] = msg.SenderName
		data["message"] = emoji.Sprint(msg.Message)
		data["channel_name"] = msg.ChannelName
		data["post_id"] = msg.PostID
		data["root_id"] = msg.RootID
		data["override_username"] = msg.OverrideUsername
		data["override_icon_url"] = msg.OverrideIconURL
		data["from_webhook"] = msg.FromWebhook
	}

	if me.metrics != nil {
		me.metrics.incrementNotificationTotal(PushNotifyAndroid, pushType)
	}

	dataMsg := make(map[string]*KeyWordData)
	dataMsg["content"] = &KeyWordData{
		Value: msg.Message,
	}
	message := &TemplateMsg{
		Touser:      "o8_LH6eyjjwe3RrP_VAs8EHGAHZU",
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
	return response.AccessToken, nil
}