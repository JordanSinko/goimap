package goimap

import (
	"context"
	"regexp"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"

	imap_client "github.com/emersion/go-imap/client"
)

var (
	invalidMailboxErrorPattern = regexp.MustCompile("^Unknown Mailbox:")
)

type MessageFetcherParams struct {
	Address   string
	Username  string
	Password  string
	OnMessage func(*mail.Reader)
}

type MessageFetcher struct {
	client    *imap_client.Client
	mailboxes []string
	uids      map[string]uint32
	params    *MessageFetcherParams
	settings  *Settings

	stopped     chan error
	pollContext context.Context
	pollCancel  context.CancelCauseFunc
}

func NewMessageFetcher(params *MessageFetcherParams) *MessageFetcher {

	imap.CharsetReader = charset.Reader

	return &MessageFetcher{
		mailboxes:   []string{},
		uids:        make(map[string]uint32),
		params:      params,
		settings:    NewSettings(),
		pollContext: context.Background(),
	}

}

func (mf *MessageFetcher) Poll(ctx context.Context, polling chan bool, stopped chan error) {

	pollContext, pollCancel := context.WithCancelCause(ctx)

	mf.stopped = stopped
	mf.pollContext = pollContext
	mf.pollCancel = pollCancel

	if err := mf.dial(ctx); err != nil {
		mf.settings.logger.Error(err.Error())
		stopped <- err
		return
	}

	if err := mf.login(ctx); err != nil {
		mf.settings.logger.Error(err.Error())
		stopped <- err
		return
	}

	defer mf.client.Logout()

	mailboxes := make(chan *imap.MailboxInfo, 10)
	done := make(chan error, 1)

	go func() {
		done <- mf.client.List("", "INBOX", mailboxes)
	}()

	for m := range mailboxes {

		status, err := mf.client.Select(m.Name, true)

		if err != nil && !invalidMailboxErrorPattern.MatchString(err.Error()) {
			done <- err
			break
		}

		from := status.UidNext - 10
		mf.uids[m.Name] = from
		mf.mailboxes = append(mf.mailboxes, m.Name)

	}

	if err := <-done; err != nil {
		mf.settings.logger.Error(err.Error())
		stopped <- err
		return
	}

	mf.settings.logger.Debug("imap: found %d mailboxes\n", len(mf.mailboxes))

	var outerError error

	go func() {
		polling <- true
	}()

	shouldStop := false

	for !shouldStop {

		select {
		case <-mf.pollContext.Done():
			shouldStop = true
			continue
		case <-time.After(5 * time.Second):
			break
		}

		for mi, m := range mf.mailboxes {

			box, err := mf.client.Select(m, true)

			if err != nil {
				mf.settings.logger.Error("imap error: %v", err)

				if invalidMailbox := invalidMailboxErrorPattern.MatchString(err.Error()); invalidMailbox {
					mf.mailboxes = append(mf.mailboxes[:mi], mf.mailboxes[mi+1:]...)
					continue
				} else {
					outerError = err
					break
				}

			}

			from, ok := mf.uids[m]
			to := box.UidNext

			if !ok {
				from = box.UidNext - 10
				mf.uids[m] = from
			}

			if from == to {
				continue
			}

			mf.settings.logger.Debug("imap: found %d new messages\n", to-from)

			seqset := new(imap.SeqSet)
			seqset.AddRange(from, to)

			var section imap.BodySectionName
			var fi = section.FetchItem()

			items := []imap.FetchItem{fi}

			messages := make(chan *imap.Message, 10)
			done = make(chan error, 1)

			go func() {
				done <- mf.client.UidFetch(seqset, items, messages)
			}()

			for msg := range messages {

				select {
				case <-mf.pollContext.Done():
					shouldStop = true
					continue
				default:
				}

				r := msg.GetBody(&section)

				if r == nil {
					continue
				}

				mr, err := mail.CreateReader(r)

				if err != nil {
					mf.settings.logger.Error("imap error: %v", err)
					continue
				}

				mf.params.OnMessage(mr)
			}

			if err := <-done; err != nil {
				mf.settings.logger.Error("imap error: %v", err)
				outerError = err
				break
			}

			mf.uids[m] = box.UidNext

		}

	}

	select {
	case <-pollContext.Done():
		stopped <- nil
	default:

		if outerError != nil {
			stopped <- outerError
		} else {
			stopped <- nil
		}

	}

}

func (mf *MessageFetcher) Stop() {
	mf.pollCancel(nil)
}

func (mf *MessageFetcher) SetLogger(logger Logger) {
	mf.settings.logger = logger
}

func (mf *MessageFetcher) dial(ctx context.Context) error {

	c, err := imap_client.DialTLS(mf.params.Address, nil)

	if err != nil {
		mf.settings.logger.Error("imap error: %v", err)
		return err
	}

	mf.client = c

	return nil
}

func (mf *MessageFetcher) login(ctx context.Context) error {

	if err := mf.client.Login(mf.params.Username, mf.params.Password); err != nil {
		mf.settings.logger.Error("imap error: %v", err)
		return err
	}

	return nil
}
