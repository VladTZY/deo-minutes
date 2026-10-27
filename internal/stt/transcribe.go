package stt

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/transcribestreaming"
	"github.com/aws/aws-sdk-go-v2/service/transcribestreaming/types"
	"github.com/aws/smithy-go"

	"deo-minutes/internal/audio"
	"deo-minutes/internal/secrets"
)

// TranscribeConfig selects region and language mode.
type TranscribeConfig struct {
	Region        string
	Language      string   // used when MultiLanguage has fewer than 2 entries
	MultiLanguage []string // e.g. ro-RO, en-US → automatic language switching
}

// NewTranscribeOpener opens Amazon Transcribe Streaming sessions with the
// given credentials (kept in memory only).
func NewTranscribeOpener(cfg TranscribeConfig, creds secrets.AWSCredentials) Opener {
	client := transcribestreaming.NewFromConfig(aws.Config{
		Region:      cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken),
	})
	return func(ctx context.Context) (Stream, error) {
		in := &transcribestreaming.StartStreamTranscriptionInput{
			MediaEncoding:        types.MediaEncodingPcm,
			MediaSampleRateHertz: aws.Int32(audio.SampleRate),
		}
		if len(cfg.MultiLanguage) >= 2 {
			in.IdentifyMultipleLanguages = true
			in.LanguageOptions = aws.String(strings.Join(cfg.MultiLanguage, ","))
			in.PreferredLanguage = types.LanguageCode(cfg.MultiLanguage[0])
		} else {
			in.LanguageCode = types.LanguageCode(cfg.Language)
		}
		resp, err := client.StartStreamTranscription(ctx, in)
		if err != nil {
			return nil, classify(err)
		}
		return newAWSStream(resp.GetStream()), nil
	}
}

// classify marks errors that a retry cannot fix.
func classify(err error) error {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		switch ae.ErrorCode() {
		case "UnrecognizedClientException", "AccessDeniedException", "InvalidSignatureException",
			"ExpiredTokenException", "BadRequestException", "InvalidClientTokenId":
			return fmt.Errorf("%w: %s", ErrFatal, ae.ErrorCode())
		}
	}
	return err
}

type awsStream struct {
	es  *transcribestreaming.StartStreamTranscriptionEventStream
	out chan []RelWord
}

func newAWSStream(es *transcribestreaming.StartStreamTranscriptionEventStream) *awsStream {
	s := &awsStream{es: es, out: make(chan []RelWord, 64)}
	go s.read()
	return s
}

func (s *awsStream) read() {
	defer close(s.out)
	for ev := range s.es.Events() {
		te, ok := ev.(*types.TranscriptResultStreamMemberTranscriptEvent)
		if !ok || te.Value.Transcript == nil {
			continue
		}
		for _, r := range te.Value.Transcript.Results {
			if r.IsPartial || len(r.Alternatives) == 0 {
				continue
			}
			words := make([]RelWord, 0, len(r.Alternatives[0].Items))
			for _, it := range r.Alternatives[0].Items {
				words = append(words, RelWord{
					Start: it.StartTime,
					End:   it.EndTime,
					Text:  aws.ToString(it.Content),
					Punct: it.Type == types.ItemTypePunctuation,
					Lang:  string(r.LanguageCode),
				})
			}
			s.out <- words
		}
	}
}

func (s *awsStream) Send(ctx context.Context, pcm []byte) error {
	err := s.es.Send(ctx, &types.AudioStreamMemberAudioEvent{Value: types.AudioEvent{AudioChunk: pcm}})
	return classify(err)
}

func (s *awsStream) Results() <-chan []RelWord { return s.out }
func (s *awsStream) Finish() error             { return s.es.Writer.Close() }
func (s *awsStream) Close() error              { return s.es.Close() }
func (s *awsStream) Err() error                { return classify(s.es.Err()) }
