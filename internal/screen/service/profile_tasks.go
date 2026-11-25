package service

import (
	"context"
	"fmt"
	"slices"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	accountsvc "github.com/shopnexus/server/internal/account/service"
)

// ProfileTasksScreen is the ProfileTasksScreen schema in
// api/schemas/screen_account.yaml.
type ProfileTasksScreen struct {
	Tasks   []ProfileTask           `json:"tasks"`
	Account accountsvc.MeView       `json:"account"`
	Address *accountsvc.AddressView `json:"address"`
}

// ProfileTask is one step of the view and whether it is done.
type ProfileTask struct {
	Key  TaskKey `json:"key"`
	Done bool    `json:"done"`
}

// TaskKey names a step of Hoàn thiện hồ sơ.
type TaskKey string

const (
	TaskAddress TaskKey = "address"
	TaskBio     TaskKey = "bio"
	TaskPhone   TaskKey = "phone"
	TaskAvatar  TaskKey = "avatar"
)

// ProfileTasks reads the Hoàn thiện hồ sơ view: the four steps, the account
// and the default address, nil when the book is empty. A deleted account is
// accountdomain.ErrSessionRevoked, from Me.
func (s *Screen) ProfileTasks(ctx context.Context, account uuid.UUID) (ProfileTasksScreen, error) {
	me, err := s.d.Accounts.Me(ctx, account)
	if err != nil {
		return ProfileTasksScreen{}, fmt.Errorf("read the account: %w", err)
	}
	book, err := s.d.Accounts.Addresses(ctx, account)
	if err != nil {
		return ProfileTasksScreen{}, fmt.Errorf("read the address book: %w", err)
	}
	// Addresses lists the default first.
	var address *accountsvc.AddressView
	if len(book) > 0 {
		address = &book[0]
	}
	hasPhone := slices.ContainsFunc(me.Identities, func(i accountsvc.Identity) bool { return i.Kind == accountdomain.KindPhone })
	return ProfileTasksScreen{
		Tasks: []ProfileTask{
			{Key: TaskAddress, Done: address != nil},
			{Key: TaskBio, Done: me.Bio != ""},
			{Key: TaskPhone, Done: hasPhone},
			{Key: TaskAvatar, Done: me.AvatarURL != ""},
		},
		Account: me,
		Address: address,
	}, nil
}
