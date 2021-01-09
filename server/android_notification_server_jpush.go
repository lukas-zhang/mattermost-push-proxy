// Copyright (c) 2015 Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package server

import (
	"time"
	"strings"
	"encoding/json"
	"github.com/ylywyn/jpush-api-go-client"
	"github.com/kyokomi/emoji"
)

type AndroidNotificationServerJ struct {
	AndroidPushSettings AndroidPushSettings
	metrics             *metrics
	logger              *Logger
}

type JPushError struct {
	code      string
	message   string
}

type JPushResponse struct {
    err     *JPushError
}

func NewAndroidNotificationServerJ(settings AndroidPushSettings, logger *Logger, metrics *metrics) NotificationServer {
	return &AndroidNotificationServerJ{
		AndroidPushSettings: settings,
		metrics:             metrics,
		logger:              logger,
	}
}

func (me *AndroidNotificationServerJ) Initialize() bool {
	me.logger.Infof("Initializing Android notification server for type=%v", me.AndroidPushSettings.Type)

	if me.AndroidPushSettings.AndroidAPIKey == "" {
		me.logger.Error("Android push notifications not configured.  Missing AndroidAPIKey.")
		return false
	}

	return true
}

func (me *AndroidNotificationServerJ) SendNotification(msg *PushNotification) PushResponse {
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

	var pf jpushclient.Platform
	pf.Add(jpushclient.ANDROID)
	var ad jpushclient.Audience
	s := []string{msg.DeviceID}
	ad.SetID(s)
	var notice jpushclient.Notice
	notice.SetAlert("New Message")
	notice.SetAndroidNotice(&jpushclient.AndroidNotice{Alert: msg.Message, Title: msg.SenderName, Extras: make(map[string]interface{})})
	notice.Android.Extras["data"] = data
	payload := jpushclient.NewPushPayLoad()
	payload.SetPlatform(&pf)
	payload.SetAudience(&ad)
        payload.SetNotice(&notice)
	bytes, _ := payload.ToBytes()
	if me.AndroidPushSettings.AndroidAPIKey != "" {
		keySec := strings.Split(me.AndroidPushSettings.AndroidAPIKey, ":")
		sender := jpushclient.NewPushClient(keySec[1], keySec[0])

		me.logger.Infof("Sending android push notification for device=%v and type=%v", me.AndroidPushSettings.Type, msg.Type)

		start := time.Now()
		resp, err := sender.Send(bytes)
		if me.metrics != nil {
			me.metrics.observerNotificationResponse(PushNotifyAndroid, time.Since(start).Seconds())
		}

		if err != nil {
			me.logger.Errorf("Failed to send J push sid=%v did=%v err=%v type=%v", msg.ServerID, msg.DeviceID, err, me.AndroidPushSettings.Type)
			if me.metrics != nil {
				me.metrics.incrementFailure(PushNotifyAndroid, pushType, "unknown transport error")
			}
			return NewErrorPushResponse("unknown transport error")
		}

		var response JPushResponse
		err = json.Unmarshal([]byte(resp), &response)
		if err != nil {
			me.logger.Errorf("Failed to unmarshal response: %v sid=%v did=%v err=%v type=%v", resp, msg.ServerID, msg.DeviceID, err, me.AndroidPushSettings.Type)
			if me.metrics != nil {
				me.metrics.incrementFailure(PushNotifyAndroid, pushType, "invalid response")
			}
			return NewErrorPushResponse("invalid response")
		}

		if response.err != nil {
			me.logger.Errorf("Failed to send J push sid=%v did=%v err=%v type=%v", msg.ServerID, msg.DeviceID, err, me.AndroidPushSettings.Type)
			if me.metrics != nil {
				me.metrics.incrementFailure(PushNotifyAndroid, pushType, response.err.message)
			}
			return NewErrorPushResponse(response.err.message)
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
