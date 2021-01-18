// Copyright (c) 2015 Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package server

import (
	"time"
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
	errcode     int
	errmsg      string
	msgid       string
}

func NewAndroidNotificationServerW(settings AndroidPushSettings, logger *Logger, metrics *metrics) NotificationServer {
	return &AndroidNotificationServerJ{
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
		post_url := "https://api.weixin.qq.com/cgi-bin/message/template/send?access_token=" + me.AndroidPushSettings.AndroidAPIKey
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
			me.logger.Errorf("Failed to send Wechat push sid=%v did=%v err=%v type=%v", msg.ServerID, msg.DeviceID, err, me.AndroidPushSettings.Type)
			if me.metrics != nil {
				me.metrics.incrementFailure(PushNotifyAndroid, pushType, "unknown transport error")
			}
			return NewErrorPushResponse("unknown transport error")
		}

		var response WPushResponse
		content, err := ioutil.ReadAll(resp.Body)
		err = json.Unmarshal([]byte(content), &response)
		if err != nil {
			me.logger.Errorf("Failed to unmarshal response: %v sid=%v did=%v err=%v type=%v", resp, msg.ServerID, msg.DeviceID, err, me.AndroidPushSettings.Type)
			if me.metrics != nil {
				me.metrics.incrementFailure(PushNotifyAndroid, pushType, "invalid response")
			}
			return NewErrorPushResponse("invalid response")
		}

		if response.errcode != 0 {
			me.logger.Errorf("Failed to send J push sid=%v did=%v err=%v type=%v", msg.ServerID, msg.DeviceID, err, me.AndroidPushSettings.Type)
			if me.metrics != nil {
				me.metrics.incrementFailure(PushNotifyAndroid, pushType, response.errmsg)
			}
			return NewErrorPushResponse(response.errmsg)
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
