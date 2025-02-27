package bot

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/demisto/alfred/conf"
	"github.com/demisto/alfred/domain"
	"github.com/demisto/alfred/util"
	"github.com/demisto/slack"
	"github.com/slavikm/govt"
)

const (
	botName            = "DBot"
	reactionTooBig     = "warning"
	reactionGood       = "+1"
	reactionBad        = "imp"
	fileCommentGood    = "File (%s) is clean. Click %s for more details."
	fileCommentBig     = "File (%s) is too large to scan. Click %s for more details."
	fileCommentBad     = "Warning: File (%s) is malicious. Click %s for more details."
	fileCommentWarning = "Unable to find details regarding this file (%s). Click %s for more details."
	urlCommentGood     = "URL (%s) is clean: %s."
	urlCommentBad      = "Warning: URL (%s) is malicious: %s."
	urlCommentWarning  = "Unable to find details regarding this URL (%s): %s."
	ipCommentGood      = "IP (%s) is clean: %s."
	ipCommentBad       = "Warning: IP (%s) is malicious: %s."
	ipCommentWarning   = "Unable to find details regarding this IP (%s): %s."
	ipCommentPrivate   = "IP (%s) is a private (internal) IP so we cannot provide reputation information: %s."
	md5CommentGood     = "MD5 hash (%s) is clean: %s."
	md5CommentBad      = "Warning: MD5 hash (%s) is malicious: %s."
	md5CommentWarning  = "Unable to find details regarding this MD5 hash (%s): %s."
	mainMessage        = "Security check by DBot - Demisto Bot. Click <%s|here> for configuration and details."
	firstMessage       = "<@%s|%s> has added <%s|DBot> by Demisto to monitor this channel."
)

func joinMap(m map[string]bool) string {
	res := ""
	for k, v := range m {
		if v {
			res += k + ","
		}
	}
	if len(res) > 0 {
		return res[0 : len(res)-1]
	}
	return res
}

func joinMapInt(m map[string]int) string {
	res := ""
	for k, v := range m {
		res += fmt.Sprintf("%s (%d),", k, v)
	}
	if len(res) > 0 {
		return res[0 : len(res)-1]
	}
	return res
}

func mainMessageFormatted() string {
	return fmt.Sprintf(mainMessage, conf.Options.ExternalAddress)
}

func (b *Bot) handleFileReply(reply *domain.WorkReply, data *domain.Context, sub *subscription, verbose bool) {
	// First, make sure it is a valid reply and if not, do nothing
	if len(reply.MD5s) != 1 {
		logrus.Warnf("Weird, invalid reply with no MD5 part - %+v", reply)
		return
	}
	link := fmt.Sprintf("%s/details?f=%s&t=%s", conf.Options.ExternalAddress, reply.File.Details.ID, data.Team)
	color := "warning"
	comment := fileCommentWarning
	shouldPost := false
	if reply.File.FileTooLarge {
		comment = fileCommentBig
		shouldPost = true
	} else if reply.File.Result == domain.ResultDirty {
		color = "danger"
		comment = fileCommentBad
	} else if reply.File.Result == domain.ResultClean {
		// At least one of reputation services found this to be known good
		// Keep the default
		color = "good"
		comment = fileCommentGood
	}
	fileMessage := fmt.Sprintf(comment, reply.File.Details.Name, fmt.Sprintf("<%s&text=%s|Details>", link, url.QueryEscape(reply.MD5s[0].Details)))
	postMessage := &slack.PostMessageRequest{
		Channel:     data.Channel,
		Attachments: []slack.Attachment{{Fallback: fileMessage, Text: fileMessage, Color: color}},
	}
	if data.Channel != "" {
		if verbose {
			shouldPost = true
			if !reply.MD5s[0].XFE.NotFound && reply.MD5s[0].XFE.Error == "" {
				xfeColor := "good"
				if len(reply.MD5s[0].XFE.Malware.Family) > 0 {
					xfeColor = "danger"
				}
				postMessage.Attachments = append(postMessage.Attachments, slack.Attachment{
					Fallback:  fmt.Sprintf("Mime Type: %s, Family: %s", reply.MD5s[0].XFE.Malware.MimeType, strings.Join(reply.MD5s[0].XFE.Malware.Family, ",")),
					Color:     xfeColor,
					Title:     "IBM X-Force Exchange",
					TitleLink: fmt.Sprintf("https://exchange.xforce.ibmcloud.com/malware/%s", reply.MD5s[0].Details),
					Fields: []slack.AttachmentField{
						slack.AttachmentField{Title: "Family", Value: strings.Join(reply.MD5s[0].XFE.Malware.Family, ","), Short: true},
						slack.AttachmentField{Title: "MIME Type", Value: reply.MD5s[0].XFE.Malware.MimeType, Short: true},
						slack.AttachmentField{Title: "Created", Value: reply.MD5s[0].XFE.Malware.Created.String(), Short: true},
					},
				})
			}
			if reply.MD5s[0].VT.FileReport.ResponseCode == 1 {
				vtColor := "good"
				if reply.MD5s[0].VT.FileReport.Positives >= numOfPositivesToConvictForFiles {
					vtColor = "danger"
				}
				postMessage.Attachments = append(postMessage.Attachments, slack.Attachment{
					Fallback:  fmt.Sprintf("Scan Date: %s, Positives: %v, Total: %v", reply.MD5s[0].VT.FileReport.ScanDate, reply.MD5s[0].VT.FileReport.Positives, reply.MD5s[0].VT.FileReport.Total),
					Color:     vtColor,
					Title:     "VirusTotal",
					TitleLink: reply.MD5s[0].VT.FileReport.Permalink,
					Fields: []slack.AttachmentField{
						slack.AttachmentField{Title: "Scan Date", Value: reply.MD5s[0].VT.FileReport.ScanDate, Short: true},
						slack.AttachmentField{Title: "Positives", Value: fmt.Sprintf("%v", reply.MD5s[0].VT.FileReport.Positives), Short: true},
						slack.AttachmentField{Title: "Total", Value: fmt.Sprintf("%v", reply.MD5s[0].VT.FileReport.Total), Short: true},
					},
				})
			}
			if reply.File.Virus != "" {
				postMessage.Attachments = append(postMessage.Attachments, slack.Attachment{
					Fallback:   fmt.Sprintf("Virus name: %s", reply.File.Virus),
					Text:       fmt.Sprintf("Virus name: %s", reply.File.Virus),
					Color:      "danger",
					AuthorName: "ClamAV",
					Title:      "ClamAV",
				})
			}
		} else if reply.File.Result != domain.ResultClean {
			shouldPost = true
		}
	}
	if shouldPost {
		err := b.post(postMessage, reply, data, sub)
		if err != nil {
			logrus.Errorf("Unable to send message to Slack - %v\n", err)
			return
		}
	}
}

func (b *Bot) handleReplyStats(reply *domain.WorkReply, ctx *domain.Context) {
	b.smu.Lock()
	defer b.smu.Unlock()
	stats := b.stats[ctx.Team]
	if stats == nil {
		stats = &domain.Statistics{Team: ctx.Team}
		b.stats[ctx.Team] = stats
	}
	stats.Messages++
	if reply.Type&domain.ReplyTypeFile > 0 {
		if reply.File.Result == domain.ResultClean {
			stats.FilesClean++
		} else if reply.File.Result == domain.ResultDirty {
			stats.FilesDirty++
		} else {
			stats.FilesUnknown++
		}
	} else {
		for i := range reply.MD5s {
			if reply.MD5s[i].Result == domain.ResultClean {
				stats.HashesClean++
			} else if reply.MD5s[i].Result == domain.ResultDirty {
				stats.HashesDirty++
			} else {
				stats.HashesUnknown++
			}
		}
		for i := range reply.URLs {
			if reply.URLs[i].Result == domain.ResultClean {
				stats.URLsClean++
			} else if reply.URLs[i].Result == domain.ResultDirty {
				stats.URLsDirty++
			} else {
				stats.URLsUnknown++
			}
		}
		for i := range reply.IPs {
			if reply.IPs[i].Result == domain.ResultClean {
				stats.IPsClean++
			} else if reply.IPs[i].Result == domain.ResultDirty {
				stats.IPsDirty++
			} else {
				stats.IPsUnknown++
			}
		}
	}
}

func (b *Bot) handleConvicted(reply *domain.WorkReply, ctx *domain.Context) {
	if reply.Type&domain.ReplyTypeFile > 0 && reply.File.Result == domain.ResultDirty {
		// First, make sure it is a valid reply and if not, do nothing
		if len(reply.MD5s) != 1 {
			logrus.Warnf("Weird, invalid reply with no MD5 part - %+v", reply)
			return
		}
		vtScore := fmt.Sprintf("%v / %v", reply.MD5s[0].VT.FileReport.Positives, reply.MD5s[0].VT.FileReport.Total)
		xfeScore := strings.Join(reply.MD5s[0].XFE.Malware.Family, ",")
		b.r.StoreMaliciousContent(&domain.MaliciousContent{
			Team:        ctx.Team,
			Channel:     ctx.Channel,
			MessageID:   reply.File.Details.ID,
			ContentType: domain.ReplyTypeFile,
			Content:     reply.MD5s[0].Details,
			FileName:    reply.File.Details.Name,
			VT:          vtScore,
			XFE:         xfeScore,
			ClamAV:      reply.File.Virus})
	} else {
		for i := range reply.MD5s {
			if reply.MD5s[i].Result == domain.ResultDirty {
				vtScore := fmt.Sprintf("%v / %v", reply.MD5s[i].VT.FileReport.Positives, reply.MD5s[i].VT.FileReport.Total)
				xfeScore := strings.Join(reply.MD5s[i].XFE.Malware.Family, ",")
				b.r.StoreMaliciousContent(&domain.MaliciousContent{
					Team:        ctx.Team,
					Channel:     ctx.Channel,
					MessageID:   reply.MessageID,
					ContentType: domain.ReplyTypeMD5,
					Content:     reply.MD5s[i].Details,
					VT:          vtScore,
					XFE:         xfeScore})
			}
		}
		for i := range reply.URLs {
			if reply.URLs[i].Result == domain.ResultDirty {
				vtScore := fmt.Sprintf("%v / %v", reply.URLs[i].VT.URLReport.Positives, reply.URLs[i].VT.URLReport.Total)
				xfeScore := fmt.Sprintf("%v", reply.URLs[i].XFE.URLDetails.Score)
				b.r.StoreMaliciousContent(&domain.MaliciousContent{
					Team:        ctx.Team,
					Channel:     ctx.Channel,
					MessageID:   reply.MessageID,
					ContentType: domain.ReplyTypeURL,
					Content:     reply.URLs[i].Details,
					VT:          vtScore,
					XFE:         xfeScore})
			}
		}
		for i := range reply.IPs {
			if reply.IPs[i].Result == domain.ResultDirty {
				vtScore := fmt.Sprintf("%v", len(reply.IPs[i].VT.IPReport.DetectedUrls))
				xfeScore := fmt.Sprintf("%v", reply.IPs[i].XFE.IPReputation.Score)
				b.r.StoreMaliciousContent(&domain.MaliciousContent{
					Team:        ctx.Team,
					Channel:     ctx.Channel,
					MessageID:   reply.MessageID,
					ContentType: domain.ReplyTypeIP,
					Content:     reply.IPs[i].Details,
					VT:          vtScore,
					XFE:         xfeScore})
			}
		}
	}
}

// IPByDate sorting
type IPByDate []govt.DetectedUrl

func (a IPByDate) Len() int           { return len(a) }
func (a IPByDate) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a IPByDate) Less(i, j int) bool { return a[i].ScanDate < a[j].ScanDate }

func (b *Bot) relevantUser(ctx *domain.Context) *subscription {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.subscriptions[ctx.Team]
}

func nilOrUnknown(v interface{}) string {
	if v == nil {
		return "Unknown"
	}
	return fmt.Sprintf("%v", v)
}

func (b *Bot) handleReply(reply *domain.WorkReply) {
	logrus.Debugf("Handling reply - %s", reply.MessageID)
	data, err := GetContext(reply.Context)
	if err != nil {
		logrus.Warnf("Error getting context from reply - %+v\n", reply)
		return
	}
	b.handleReplyStats(reply, data)
	b.handleConvicted(reply, data)
	sub := b.relevantUser(data)
	if sub == nil {
		logrus.Warnf("Team not found in subscriptions for message %s", reply.MessageID)
	}
	verbose := false
	if data.Channel != "" {
		if data.Channel[0] == 'D' {
			// Since it's a direct message to me, I need to reply verbose
			verbose = true
		} else {
			verbose = sub.configuration.IsVerbose(data.Channel, "")
		}
	}
	if reply.Type&domain.ReplyTypeFile > 0 {
		b.handleFileReply(reply, data, sub, verbose)
	} else {
		link := fmt.Sprintf("%s/details?c=%s&m=%s&t=%s", conf.Options.ExternalAddress, data.Channel, reply.MessageID, data.Team)
		postMessage := &slack.PostMessageRequest{Channel: data.Channel}
		for i := range reply.URLs {
			color := "warning"
			comment := urlCommentWarning
			if reply.URLs[i].Result == domain.ResultDirty {
				color = "danger"
				comment = urlCommentBad
			} else if reply.URLs[i].Result == domain.ResultClean {
				color = "good"
				comment = urlCommentGood
			}
			urlMessage := fmt.Sprintf(comment, reply.URLs[i].Details, fmt.Sprintf("<%s&text=%s|Details>", link, url.QueryEscape("<"+reply.URLs[i].Details+">")))
			if verbose || color != "good" {
				postMessage.Attachments = append(postMessage.Attachments, slack.Attachment{
					Fallback: urlMessage,
					Text:     urlMessage,
					Color:    color,
				})
			}
			if verbose {
				if !reply.URLs[i].XFE.NotFound && reply.URLs[i].XFE.Error == "" {
					xfeColor := "good"
					if reply.URLs[i].XFE.URLDetails.Score >= xfeScoreToConvict {
						xfeColor = "danger"
					}
					postMessage.Attachments = append(postMessage.Attachments, slack.Attachment{
						Fallback: fmt.Sprintf("Score: %v, A Records: %s, Categories: %s",
							reply.URLs[i].XFE.URLDetails.Score,
							strings.Join(reply.URLs[i].XFE.Resolve.A, ","),
							joinMap(reply.URLs[i].XFE.URLDetails.Cats)),
						Color:     xfeColor,
						Title:     "IBM X-Force Exchange",
						TitleLink: fmt.Sprintf("https://exchange.xforce.ibmcloud.com/url/%s", reply.URLs[i].Details),
						Fields: []slack.AttachmentField{
							slack.AttachmentField{Title: "Score", Value: fmt.Sprintf("%v", reply.URLs[i].XFE.URLDetails.Score), Short: true},
							slack.AttachmentField{Title: "A Records", Value: strings.Join(reply.URLs[i].XFE.Resolve.A, ","), Short: true},
							slack.AttachmentField{Title: "Categories", Value: joinMap(reply.URLs[i].XFE.URLDetails.Cats), Short: true},
						},
					})
					if len(reply.URLs[i].XFE.Resolve.AAAA) > 0 {
						postMessage.Attachments[len(postMessage.Attachments)-1].Fields = append(postMessage.Attachments[len(postMessage.Attachments)-1].Fields,
							slack.AttachmentField{Title: "A Records", Value: strings.Join(reply.URLs[i].XFE.Resolve.AAAA, ","), Short: true})
					}
				}
				if reply.URLs[i].VT.URLReport.ResponseCode == 1 {
					vtColor := "good"
					if reply.URLs[i].VT.URLReport.Positives >= numOfPositivesToConvict {
						vtColor = "danger"
					}
					postMessage.Attachments = append(postMessage.Attachments, slack.Attachment{
						Fallback:  fmt.Sprintf("Scan Date: %s, Positives: %v, Total: %v", reply.URLs[i].VT.URLReport.ScanDate, reply.URLs[i].VT.URLReport.Positives, reply.URLs[i].VT.URLReport.Total),
						Color:     vtColor,
						Title:     "VirusTotal",
						TitleLink: reply.URLs[i].VT.URLReport.Permalink,
						Fields: []slack.AttachmentField{
							slack.AttachmentField{Title: "Scan Date", Value: reply.URLs[i].VT.URLReport.ScanDate, Short: true},
							slack.AttachmentField{Title: "Positives", Value: fmt.Sprintf("%v", reply.URLs[i].VT.URLReport.Positives), Short: true},
							slack.AttachmentField{Title: "Total", Value: fmt.Sprintf("%v", reply.URLs[i].VT.URLReport.Total), Short: true},
						},
					})
				}
			}
		}
		for i := range reply.IPs {
			color := "warning"
			comment := ipCommentWarning
			if reply.IPs[i].Private {
				color = "good"
				comment = ipCommentPrivate
			} else if reply.IPs[i].Result == domain.ResultDirty {
				color = "danger"
				comment = ipCommentBad
			} else if reply.IPs[i].Result == domain.ResultClean {
				color = "good"
				comment = ipCommentGood
			}
			ipMessage := fmt.Sprintf(comment, reply.IPs[i].Details, fmt.Sprintf("<%s&text=%s|Details>", link, url.QueryEscape(reply.IPs[i].Details)))
			if verbose || color != "good" {
				postMessage.Attachments = append(postMessage.Attachments, slack.Attachment{
					Fallback: ipMessage,
					Text:     ipMessage,
					Color:    color,
				})
			}
			if verbose {
				if !reply.IPs[i].XFE.NotFound && reply.IPs[i].XFE.Error == "" {
					xfeColor := "good"
					if reply.IPs[i].XFE.IPReputation.Score >= xfeScoreToConvict {
						xfeColor = "danger"
					}
					postMessage.Attachments = append(postMessage.Attachments, slack.Attachment{
						Fallback: fmt.Sprintf("Score: %v, Categories: %s, Geo: %v",
							reply.IPs[i].XFE.IPReputation.Score, joinMapInt(reply.IPs[i].XFE.IPReputation.Cats), nilOrUnknown(reply.IPs[i].XFE.IPReputation.Geo["country"])),
						Color:     xfeColor,
						Title:     "IBM X-Force Exchange",
						TitleLink: fmt.Sprintf("https://exchange.xforce.ibmcloud.com/ip/%s", reply.IPs[i].Details),
						Fields: []slack.AttachmentField{
							slack.AttachmentField{Title: "Score", Value: fmt.Sprintf("%v", reply.IPs[i].XFE.IPReputation.Score), Short: true},
							slack.AttachmentField{Title: "Categories", Value: joinMapInt(reply.IPs[i].XFE.IPReputation.Cats), Short: true},
							slack.AttachmentField{Title: "Geo", Value: nilOrUnknown(reply.IPs[i].XFE.IPReputation.Geo["country"]), Short: true},
						},
					})
				}
				if reply.IPs[i].VT.IPReport.ResponseCode == 1 {
					var vtPositives uint16
					listOfURLs := ""
					now := time.Now()
					detectedURLs := reply.IPs[i].VT.IPReport.DetectedUrls
					sort.Sort(sort.Reverse(IPByDate(detectedURLs)))
					for j := range detectedURLs {
						t, err := time.Parse("2006-01-02 15:04:05", detectedURLs[j].ScanDate)
						if err != nil {
							logrus.Debugf("Error parsing scan date - %v", err)
							continue
						}
						if detectedURLs[j].Positives > vtPositives && t.Add(365*24*time.Hour).After(now) {
							vtPositives = detectedURLs[j].Positives
						}
						if j < 20 {
							listOfURLs += fmt.Sprintf("URL: %s, Positives: %v, Total: %v, Date: %s", detectedURLs[j].Url, detectedURLs[j].Positives, detectedURLs[j].Total, detectedURLs[j].ScanDate) + "\n"
						}
					}
					vtColor := "good"
					if vtPositives >= numOfPositivesToConvict {
						vtColor = "danger"
					}
					postMessage.Attachments = append(postMessage.Attachments, slack.Attachment{
						Fallback:  listOfURLs,
						Text:      listOfURLs,
						Color:     vtColor,
						Title:     "VirusTotal",
						TitleLink: "https://www.virustotal.com/en/search?query=" + reply.IPs[i].Details,
					})
				}
			}
		}
		// We will handle hashes only for verbose channels
		if verbose {
			for i := range reply.MD5s {
				color := "warning"
				comment := md5CommentWarning
				if reply.MD5s[i].Result == domain.ResultDirty {
					color = "danger"
					comment = md5CommentBad
				} else if reply.MD5s[i].Result == domain.ResultClean {
					color = "good"
					comment = md5CommentGood
				}
				md5Message := fmt.Sprintf(comment, reply.MD5s[i].Details, fmt.Sprintf("<%s&text=%s|Details>", link, url.QueryEscape(reply.MD5s[i].Details)))
				postMessage.Attachments = append(postMessage.Attachments, slack.Attachment{
					Fallback: md5Message,
					Text:     md5Message,
					Color:    color,
				})
				if !reply.MD5s[i].XFE.NotFound && reply.MD5s[i].XFE.Error == "" {
					xfeColor := "good"
					if len(reply.MD5s[i].XFE.Malware.Family) > 0 {
						xfeColor = "danger"
					}
					postMessage.Attachments = append(postMessage.Attachments, slack.Attachment{
						Fallback:  fmt.Sprintf("Mime Type: %s, Family: %s", reply.MD5s[i].XFE.Malware.MimeType, strings.Join(reply.MD5s[i].XFE.Malware.Family, ",")),
						Color:     xfeColor,
						Title:     "IBM X-Force Exchange",
						TitleLink: fmt.Sprintf("https://exchange.xforce.ibmcloud.com/malware/%s", reply.MD5s[i].Details),
						Fields: []slack.AttachmentField{
							slack.AttachmentField{Title: "Family", Value: strings.Join(reply.MD5s[i].XFE.Malware.Family, ","), Short: true},
							slack.AttachmentField{Title: "MIME Type", Value: reply.MD5s[i].XFE.Malware.MimeType, Short: true},
							slack.AttachmentField{Title: "Created", Value: reply.MD5s[i].XFE.Malware.Created.String(), Short: true},
						},
					})
				}
				if reply.MD5s[i].VT.FileReport.ResponseCode == 1 {
					vtColor := "good"
					if reply.MD5s[i].VT.FileReport.Positives >= numOfPositivesToConvictForFiles {
						vtColor = "danger"
					}
					postMessage.Attachments = append(postMessage.Attachments, slack.Attachment{
						Fallback:  fmt.Sprintf("Scan Date: %s, Positives: %v, Total: %v", reply.MD5s[i].VT.FileReport.ScanDate, reply.MD5s[i].VT.FileReport.Positives, reply.MD5s[i].VT.FileReport.Total),
						Color:     vtColor,
						Title:     "VirusTotal",
						TitleLink: reply.MD5s[i].VT.FileReport.Permalink,
						Fields: []slack.AttachmentField{
							slack.AttachmentField{Title: "Scan Date", Value: reply.MD5s[i].VT.FileReport.ScanDate, Short: true},
							slack.AttachmentField{Title: "Positives", Value: fmt.Sprintf("%v", reply.MD5s[i].VT.FileReport.Positives), Short: true},
							slack.AttachmentField{Title: "Total", Value: fmt.Sprintf("%v", reply.MD5s[i].VT.FileReport.Total), Short: true},
						},
					})
				}
			}
		}
		clean := true
		if !verbose {
			for i := range postMessage.Attachments {
				if postMessage.Attachments[i].Color != "good" {
					clean = false
					break
				}
			}
		}
		if verbose || !clean {
			err = b.post(postMessage, reply, data, sub)
			if err != nil {
				logrus.Errorf("Unable to send message to Slack - %v\n", err)
				return
			}
		} else {
			logrus.Debugf("Reply %s clean, ignoring", reply.MessageID)
		}
	}
}

// post uses the correct client to post to the channel
// See if the original message poster is subscribed and if so use him.
// If not, use the first user we have that is subscribed to the channel.
func (b *Bot) post(message *slack.PostMessageRequest, reply *domain.WorkReply, data *domain.Context, sub *subscription) error {
	message.Text = mainMessageFormatted()
	message.AsUser = true
	var err error
	_, err = sub.s.PostMessage(message, false)
	return err
}

func parseChannels(sub *subscription, text string, pos int) ([]string, []string, error) {
	parts := strings.Split(text, " ")
	if len(parts) <= pos {
		return nil, nil, fmt.Errorf("Not enough parameters in '%s'", text)
	}
	var channels []string
	for i := pos; i < len(parts); i++ {
		subparts := strings.Split(parts[i], ",")
		for j := range subparts {
			subpart := strings.TrimSpace(subparts[j])
			if subpart != "" {
				var ch string
				if strings.Contains(subpart, "<#") { // if this is #channel
					ch = subpart[2 : len(subpart)-1]
				} else {
					ch = sub.ChannelID(subpart)
				}
				if ch != "" {
					channels = append(channels, ch)
				}
			}
		}
	}
	return parts, channels, nil
}

func (b *Bot) joinChannels(team, text, channel string) {
	postMessage := &slack.PostMessageRequest{
		Channel: channel,
		AsUser:  true,
	}
	sub := b.subscriptions[team]
	if sub == nil {
		logrus.Warnf("Got message but do not have subsciption for team %s", team)
		return
	}
	users, err := b.r.TeamMembers(team)
	if err != nil {
		logrus.Warnf("Unable to retrieve team members - %v", err)
		return
	}
	parts, incomingChannels, err := parseChannels(sub, text, 1)
	ch, err := sub.s.ChannelList(true)
	if err != nil {
		logrus.Warnf("Error retrieving my channels - %v", err)
		postMessage.Text = "Error retrieving current configuration. Rest assured we are looking into the issue."
	} else {
		var channels []string
		var channelFound bool
	users_loop:
		for i := range users {
			if users[i].Status == domain.UserStatusActive {
				s, err := slack.New(slack.SetToken(users[i].Token))
				if err != nil {
					logrus.Infof("Error creating Slack client for user %s (%s) - %v\n", users[i].ID, users[i].Name, err)
					continue
				}
				for i := range ch.Channels {
					if !ch.Channels[i].IsMember && !util.In(channels, ch.Channels[i].Name) &&
						(strings.ToLower(parts[1]) == "all" || util.In(incomingChannels, ch.Channels[i].ID)) {
						channelFound = true
						_, err = s.ChannelInvite(ch.Channels[i].ID, sub.team.BotUserID)
						if err != nil {
							logrus.Infof("Error inviting us - %v\n", err)
							continue users_loop
						}
						channels = append(channels, ch.Channels[i].Name)
					}
				}
				break
			}
		}
		if len(channels) > 0 {
			text := fmt.Sprintf("I've started monitoring the following channels: %s", strings.Join(channels, ", "))
			postMessage.Text = text
		} else {
			if channelFound {
				postMessage.Text = "I could not invite myself to the public channels, rest assured we are looking into the issue."
			} else {
				postMessage.Text = "I was already monitoring all public channels but thanks for thinking of me."
			}
		}
	}
	_, err = sub.s.PostMessage(postMessage, false)
	if err != nil {
		logrus.Warnf("Error posting config message - %v", err)
	}
}

func (b *Bot) handleVerbose(team, text, channel string) {
	postMessage := &slack.PostMessageRequest{
		Channel: channel,
		AsUser:  true,
	}
	sub := b.subscriptions[team]
	if sub == nil {
		logrus.Warnf("Got message but do not have subsciption for team %s", team)
		return
	}
	changed := false
	b.mu.Lock()
	defer b.mu.Unlock()
	parts, channels, err := parseChannels(sub, text, 2)
	if err != nil {
		postMessage.Text = "I could not understand your command. Verbose command is:\nverbose on #channel1,#channel2 - to turn on verbose mode on for a list of channels.\nverbose off #channel1,#channel2 - to turn off verbose mode on for a list of channels."
	} else {
		for _, ch := range channels {
			if strings.ToLower(parts[1]) == "on" && !util.In(sub.configuration.VerboseChannels, ch) {
				sub.configuration.VerboseChannels = append(sub.configuration.VerboseChannels, ch)
				changed = true
			} else if strings.ToLower(parts[1]) == "off" && util.In(sub.configuration.VerboseChannels, ch) {
				index := util.Index(sub.configuration.VerboseChannels, ch)
				if index >= 0 {
					sub.configuration.VerboseChannels = sub.configuration.VerboseChannels[:index+copy(sub.configuration.VerboseChannels[index:], sub.configuration.VerboseChannels[index+1:])]
				}
				changed = true
			}
		}
	}
	if changed {
		err := b.r.SetChannelsAndGroups(team, sub.configuration)
		if err != nil {
			logrus.Warnf("Error storing verbose configuration for team %s - %v", team, err)
			postMessage.Text = "I had an issue saving the verbose state."
		} else {
			postMessage.Text = "Verbose state was changed."
		}
	}
	_, err = sub.s.PostMessage(postMessage, false)
	if err != nil {
		logrus.Warnf("Error posting config message - %v", err)
	}
}

func (b *Bot) handleConfig(team, channel string) {
	postMessage := &slack.PostMessageRequest{
		Channel: channel,
		AsUser:  true,
	}
	sub := b.subscriptions[team]
	if sub == nil {
		logrus.Warnf("Got message but do not have subsciption for team %s", team)
		return
	}
	ch, err := sub.s.ChannelList(true)
	if err != nil {
		logrus.Warnf("Error retrieving my channels - %v", err)
		postMessage.Text = "Error retrieving configuration. Rest assured we are looking into the issue."
	} else {
		var channels []string
		var verboseChannels []string
		for i := range ch.Channels {
			if ch.Channels[i].IsMember {
				if sub.configuration.IsVerbose(ch.Channels[i].ID, "") {
					verboseChannels = append(verboseChannels, ch.Channels[i].Name)
				} else {
					channels = append(channels, ch.Channels[i].Name)
				}
			}
		}
		text := fmt.Sprintf("Channels I'm monitoring: %s", strings.Join(channels, ", "))
		if len(verboseChannels) > 0 {
			text = text + fmt.Sprintf("\nChannels I'm monitoring and providing extra info: %s", strings.Join(verboseChannels, ", "))
		}
		postMessage.Text = text
	}
	_, err = sub.s.PostMessage(postMessage, false)
	if err != nil {
		logrus.Warnf("Error posting config message - %v", err)
	}
}

func (b *Bot) showHelp(team, channel string) {
	postMessage := &slack.PostMessageRequest{
		Channel: channel,
		AsUser:  true,
		Text: `Here are the commands I understand:
config: list the current channels I'm listening on
join all/#channel1,#channel2...: I will join all/specified public channels and start monitoring them.
verbose on/off #channel1,#channel2... - turn on verbose mode on the specified channels
verbose mode is usually used by security professionals. When in verbose mode, dbot will display reputation details about any URL, IP or file including clean ones.`,
	}
	sub := b.subscriptions[team]
	var err error
	_, err = sub.s.PostMessage(postMessage, false)
	if err != nil {
		logrus.Warnf("Error posting config message - %v", err)
	}
}
