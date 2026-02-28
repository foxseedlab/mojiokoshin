package session

import "fmt"

const (
	slashCommandStartDescription = "あなたがいるボイスチャンネルで文字起こしを開始します。"
	slashCommandStopDescription  = "あなたがいるボイスチャンネルの文字起こしを中止します。"

	messageEphemeralWrongGuild        = ":warning: **このサーバーでは実行できません。**"
	messageEphemeralUnknownCommand    = ":warning: **不明なコマンドです。**"
	messageEphemeralVoiceLookupFailed = ":warning: **ボイスチャンネルの参加状態の確認に失敗しました。**"
	messageEphemeralJoinVCFirst       = ":warning: **ボイスチャンネルに参加してから実行してください。**"
	messageEphemeralAlreadyRunning    = ":warning: **このボイスチャンネルでは既に文字起こしが実行中です。**"
	messageEphemeralStartFailed       = ":warning: **文字起こしの開始に失敗しました。**"
	messageEphemeralStopFailed        = ":warning: **文字起こしの停止に失敗しました。**"
	messageEphemeralNotRunning        = ":warning: **現在このボイスチャンネルでは文字起こしは実行されていません。**"
	messagePoweredByLine              = "-# *Powered by [Mojiokoshin](https://github.com/foxseedlab/mojiokoshin)*"

	messageStartChannelTitle = ":microphone2: **文字起こしを開始しました。**"
	messageStartChannelHint  = "-# /mojiokoshi-stop コマンドで中止できます。"

	messageStopChannelTitle = ":pause_button:  **文字起こしを中止しました。**"
	messageStopRestart      = "/mojiokoshi コマンドで開始できます。"
	messageStopRestartAgain = "/mojiokoshi コマンドで再度開始できます。"

	messageAttachmentTitle = ":page_facing_up:  **文字起こしの内容**"

	messageStartEphemeralTitleFormat = ":microphone2: <#%s> **の文字起こしを開始しました。**"
	messageStopEphemeralTitleFormat  = ":pause_button:  <#%s> **の文字起こしを中止しました。**"

	messageStartEphemeralSecondLine = "-# ボイスチャンネルのチャットに文字起こしが表示されます。"
	messageStartEphemeralHint       = "-# /mojiokoshi-stop コマンドで中止できます。"
	messageStopEphemeralHint        = "-# /mojiokoshi コマンドで開始できます。"
)

func startEphemeralTitle(channelID string) string {
	return fmt.Sprintf(messageStartEphemeralTitleFormat, channelID)
}

func stopEphemeralTitle(channelID string) string {
	return fmt.Sprintf(messageStopEphemeralTitleFormat, channelID)
}

func stopReasonDetail(reason string) string {
	switch reason {
	case stopReasonMaxDuration:
		return "文字起こしの最大制限時間に到達しました。"
	case stopReasonManualSlash:
		return "参加者に終了コマンドを実行されました。"
	case stopReasonParticipantsLeft:
		return "ボイスチャットに誰もいなくなりました。"
	case stopReasonBotRemoved:
		return "文字起こしボットが退出させられました。"
	case stopReasonServerClosed:
		return "文字起こしサーバーが閉じられました。"
	case stopReasonUnknownError:
		return "不明なエラーが発生しました。"
	default:
		return "不明なエラーが発生しました。"
	}
}

func stopReasonNeedsRestartAgain(reason string) bool {
	switch reason {
	case stopReasonMaxDuration, stopReasonServerClosed, stopReasonUnknownError:
		return true
	default:
		return false
	}
}
