package session

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/foxseedlab/mojiokoshin/internal/discord"
	"github.com/foxseedlab/mojiokoshin/internal/repository"
	"github.com/foxseedlab/mojiokoshin/internal/webhook"
)

// 変更容易性を高めるため、time.DateTime をあえて指定していない
const transcriptTimeLayout = "2006-01-02 15:04:05"

func buildTranscriptText(meta discord.TranscriptMetadata, startedAt, endedAt time.Time, timezone string, loc *time.Location, segments []repository.TranscriptSegment) []byte {
	participants := canonicalParticipants(meta.Participants)
	names := make([]string, 0, len(participants))
	for _, p := range participants {
		names = append(names, p.DisplayName)
	}

	startText := startedAt.In(safeLocation(loc)).Format(transcriptTimeLayout)
	endText := endedAt.In(safeLocation(loc)).Format(transcriptTimeLayout)

	lines := []string{
		fmt.Sprintf("サーバー名：%s", meta.DiscordServerName),
		fmt.Sprintf("ボイスチャンネル名：%s", meta.DiscordVoiceChannelName),
		fmt.Sprintf("ボイスチャット期間：%s ~ %s（%s）", startText, endText, timezone),
		fmt.Sprintf("参加者：%s", strings.Join(names, "、")),
		"",
	}
	for _, seg := range segments {
		elapsed := seg.SpokenAt.Sub(startedAt)
		if elapsed < 0 {
			elapsed = 0
		}
		lines = append(lines, fmt.Sprintf("%s %s", formatElapsedHMS(elapsed), seg.Content))
	}
	return []byte(strings.Join(lines, "\n"))
}

func buildTranscriptWebhookPayload(sessionID string, meta discord.TranscriptMetadata, startedAt, endedAt time.Time, timezone string, loc *time.Location, segments []repository.TranscriptSegment) webhook.TranscriptWebhookPayload {
	participants := canonicalParticipants(meta.Participants)
	participantNames := make([]string, 0, len(participants))
	details := make([]webhook.TranscriptWebhookParticipant, 0, len(participants))
	for _, p := range participants {
		participantNames = append(participantNames, p.DisplayName)
		details = append(details, webhook.TranscriptWebhookParticipant{
			UserID:      p.UserID,
			DisplayName: p.DisplayName,
			IsBot:       p.IsBot,
		})
	}
	transcriptLines := make([]string, 0, len(segments))
	for _, seg := range segments {
		transcriptLines = append(transcriptLines, seg.Content)
	}

	durationSeconds := int64(endedAt.Sub(startedAt).Seconds())
	if durationSeconds < 0 {
		durationSeconds = 0
	}

	return webhook.TranscriptWebhookPayload{
		SchemaVersion:           webhook.TranscriptWebhookSchemaVersion,
		SessionID:               sessionID,
		DiscordServerID:         meta.DiscordServerID,
		DiscordServerName:       meta.DiscordServerName,
		DiscordVoiceChannelID:   meta.DiscordVoiceChannelID,
		DiscordVoiceChannelName: meta.DiscordVoiceChannelName,
		StartAt:                 startedAt.In(safeLocation(loc)).Format(time.RFC3339),
		EndAt:                   endedAt.In(safeLocation(loc)).Format(time.RFC3339),
		Timezone:                timezone,
		DurationSeconds:         durationSeconds,
		Participants:            participantNames,
		ParticipantDetails:      details,
		SegmentCount:            len(segments),
		TranscriptSegments:      buildTranscriptWebhookSegments(segments, endedAt, safeLocation(loc)),
		Transcript:              strings.Join(transcriptLines, "\n"),
	}
}

func buildTranscriptWebhookSegments(segments []repository.TranscriptSegment, sessionEndedAt time.Time, loc *time.Location) []webhook.TranscriptWebhookSegment {
	out := make([]webhook.TranscriptWebhookSegment, 0, len(segments))
	for i, seg := range segments {
		segmentEnd := sessionEndedAt
		if i+1 < len(segments) {
			segmentEnd = segments[i+1].SpokenAt
		}
		if segmentEnd.Before(seg.SpokenAt) {
			segmentEnd = seg.SpokenAt
		}
		out = append(out, webhook.TranscriptWebhookSegment{
			Index:      seg.SegmentIndex,
			StartAt:    seg.SpokenAt.In(loc).Format(time.RFC3339),
			EndAt:      segmentEnd.In(loc).Format(time.RFC3339),
			Transcript: seg.Content,
		})
	}
	return out
}

func canonicalParticipants(participants []discord.TranscriptParticipant) []discord.TranscriptParticipant {
	byUserID := make(map[string]discord.TranscriptParticipant, len(participants))
	for _, p := range participants {
		if strings.TrimSpace(p.UserID) == "" {
			continue
		}
		byUserID[p.UserID] = mergeParticipant(byUserID[p.UserID], p)
	}
	list := make([]discord.TranscriptParticipant, 0, len(byUserID))
	for _, p := range byUserID {
		if p.DisplayName == "" {
			p.DisplayName = p.UserID
		}
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool {
		in := strings.ToLower(list[i].DisplayName)
		jn := strings.ToLower(list[j].DisplayName)
		if in != jn {
			return in < jn
		}
		return list[i].UserID < list[j].UserID
	})
	return list
}

func mergeParticipant(existing, incoming discord.TranscriptParticipant) discord.TranscriptParticipant {
	if existing.UserID == "" {
		if incoming.DisplayName == "" {
			incoming.DisplayName = incoming.UserID
		}
		return incoming
	}
	if existing.DisplayName == existing.UserID && incoming.DisplayName != "" {
		existing.DisplayName = incoming.DisplayName
	}
	existing.IsBot = existing.IsBot || incoming.IsBot
	return existing
}

func formatElapsedHMS(d time.Duration) string {
	total := int64(d / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func safeLocation(loc *time.Location) *time.Location {
	if loc == nil {
		return time.UTC
	}
	return loc
}
