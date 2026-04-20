package accountbiz

import (
	"encoding/json"
	"strings"

	restate "github.com/restatedev/sdk-go"

	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	accountmodel "shopnexus-server/internal/module/account/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/ptrutil"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListProfileParams struct {
	sharedmodel.PaginationParams

	Issuer     accountmodel.AuthenticatedAccount // Who is requesting the profiles
	AccountIDs []uuid.UUID                       `validate:"dive,required"`
}

// ListProfile returns a paginated list of profiles for the given account IDs.
func (b *AccountHandler) ListProfile(
	ctx restate.Context,
	params ListProfileParams,
) (sharedmodel.PaginateResult[accountmodel.Profile], error) {
	var result sharedmodel.PaginateResult[accountmodel.Profile]
	if err := validator.Validate(params); err != nil {
		return result, sharedmodel.WrapErr("list profiles", err)
	}

	listProfile, err := b.storage.Querier().ListCountProfile(ctx, accountdb.ListCountProfileParams{
		ID:     params.AccountIDs,
		Limit:  params.Limit,
		Offset: params.Offset(),
	})
	if err != nil {
		return result, err
	}

	var total null.Int64
	if len(listProfile) > 0 {
		total.SetValid(listProfile[0].TotalCount)
	}

	dbProfiles := lo.Map(listProfile, func(row accountdb.ListCountProfileRow, _ int) accountdb.AccountProfile {
		return row.AccountProfile
	})

	listAccount, err := b.storage.Querier().ListAccount(ctx, accountdb.ListAccountParams{
		ID: lo.Map(params.AccountIDs, func(id uuid.UUID, _ int) uuid.UUID { return id }),
	})
	if err != nil {
		return result, err
	}

	accountMap := lo.KeyBy(listAccount, func(account accountdb.AccountAccount) uuid.UUID {
		return account.ID
	})

	profiles := make([]accountmodel.Profile, 0, len(dbProfiles))
	for _, dbProfile := range dbProfiles {
		account := accountMap[dbProfile.ID]
		profiles = append(profiles, b.mapProfile(ctx, account, dbProfile))
	}

	return sharedmodel.PaginateResult[accountmodel.Profile]{
		PageParams: params.PaginationParams,
		Data:       profiles,
		Total:      total,
	}, nil
}

type GetProfileParams struct {
	Issuer    accountmodel.AuthenticatedAccount // Who is requesting the profile
	AccountID uuid.UUID
}

// GetProfile returns the profile for the given account ID.
func (b *AccountHandler) GetProfile(ctx restate.Context, params GetProfileParams) (accountmodel.Profile, error) {
	var zero accountmodel.Profile
	profile, err := b.storage.Querier().GetProfile(ctx, accountdb.GetProfileParams{
		ID: uuid.NullUUID{UUID: params.AccountID, Valid: true},
	})
	if err != nil {
		return zero, err
	}

	account, err := b.storage.Querier().GetAccount(ctx, accountdb.GetAccountParams{
		ID: uuid.NullUUID{UUID: params.AccountID, Valid: true},
	})
	if err != nil {
		return zero, err
	}

	m := b.mapProfile(ctx, account, profile)
	return m, nil
}

type UpdateProfileParams struct {
	Issuer    accountmodel.AuthenticatedAccount // Who is performing the update
	AccountID uuid.UUID                         // Whose profile to be updated

	// Account base fields
	Status   accountdb.AccountStatus
	Username null.String
	Phone    null.String
	Email    null.String

	// Profile fields
	Gender           accountdb.AccountGender
	Name             null.String
	DateOfBirth      null.Time
	AvatarRsID       uuid.NullUUID
	DefaultContactID uuid.NullUUID

	// Description
	Description null.String
}

// UpdateProfile updates the account and profile fields for the given account.
func (b *AccountHandler) UpdateProfile(ctx restate.Context, params UpdateProfileParams) (accountmodel.Profile, error) {
	var zero accountmodel.Profile

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	account, err := b.storage.Querier().UpdateAccount(ctx, accountdb.UpdateAccountParams{
		ID:       params.AccountID,
		Status:   accountdb.NullAccountStatus{AccountStatus: params.Status, Valid: params.Status != ""},
		Username: params.Username,
		Phone:    params.Phone,
		Email:    params.Email,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("update profile", err)
	}

	profile, err := b.storage.Querier().UpdateProfile(ctx, accountdb.UpdateProfileParams{
		ID:               params.AccountID,
		Gender:           accountdb.NullAccountGender{AccountGender: params.Gender, Valid: params.Gender != ""},
		Name:             params.Name,
		DateOfBirth:      params.DateOfBirth,
		AvatarRsID:       params.AvatarRsID,
		DefaultContactID: params.DefaultContactID,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("update profile", err)
	}

	m := b.mapProfile(ctx, account, profile)
	return m, nil
}

// mapProfile maps DB account + profile rows to the model type.
func (b *AccountHandler) mapProfile(
	ctx restate.Context,
	account accountdb.AccountAccount,
	profile accountdb.AccountProfile,
) accountmodel.Profile {
	avatar, _ := b.common.GetResourceByID(ctx, profile.AvatarRsID.UUID)
	var url null.String
	if avatar != nil {
		url.SetValid(avatar.Url)
	}

	var settings accountmodel.ProfileSettings
	if len(profile.Settings) > 0 {
		_ = json.Unmarshal(profile.Settings, &settings) // tolerate invalid
	}

	return accountmodel.Profile{
		ID:          account.ID,
		DateCreated: account.DateCreated,
		DateUpdated: account.DateUpdated,

		Status:   account.Status,
		Phone:    account.Phone,
		Email:    account.Email,
		Username: account.Username,

		Gender:           ptrutil.PtrIf(profile.Gender.AccountGender, profile.Gender.Valid),
		Name:             profile.Name,
		DateOfBirth:      profile.DateOfBirth,
		EmailVerified:    profile.EmailVerified,
		PhoneVerified:    profile.PhoneVerified,
		DefaultContactID: profile.DefaultContactID,
		AvatarURL:        url,

		Settings: settings,
	}
}

// MergeSettings overlays typed fields from patch on top of existing JSONB,
// preserving unknown keys. Exported for direct testing.
func MergeSettings(existing json.RawMessage, patch accountmodel.ProfileSettings) (json.RawMessage, error) {
	merged := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &merged); err != nil {
			merged = map[string]any{} // tolerate invalid; start fresh
		}
	}
	if patch.PreferredCurrency != "" {
		merged["preferred_currency"] = patch.PreferredCurrency
	}
	return json.Marshal(merged)
}

type UpdateProfileSettingsParams struct {
	Issuer            accountmodel.AuthenticatedAccount
	AccountID         uuid.UUID   `validate:"required"`
	PreferredCurrency null.String `validate:"omitempty,iso4217"`
}

// UpdateProfileSettings patches the profile.settings JSONB. Only the
// authenticated user can modify their own settings. Unknown keys in the
// existing JSONB are preserved.
func (b *AccountHandler) UpdateProfileSettings(
	ctx restate.Context, params UpdateProfileSettingsParams,
) (accountmodel.ProfileSettings, error) {
	var zero accountmodel.ProfileSettings
	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("update profile settings", err)
	}
	if params.Issuer.ID != params.AccountID {
		return zero, accountmodel.ErrForbidden
	}

	patch := accountmodel.ProfileSettings{}
	if params.PreferredCurrency.Valid {
		code := strings.ToUpper(params.PreferredCurrency.String)
		ok, err := b.common.IsSupportedCurrency(ctx, code)
		if err != nil {
			return zero, sharedmodel.WrapErr("update profile settings", err)
		}
		if !ok {
			return zero, accountmodel.ErrUnsupportedCurrency
		}
		patch.PreferredCurrency = code
	}

	prof, err := b.storage.Querier().GetProfile(ctx, accountdb.GetProfileParams{
		ID: uuid.NullUUID{UUID: params.AccountID, Valid: true},
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("update profile settings", err)
	}

	merged, err := MergeSettings(prof.Settings, patch)
	if err != nil {
		return zero, sharedmodel.WrapErr("update profile settings", err)
	}

	updated, err := b.storage.Querier().UpdateProfileSettings(ctx, accountdb.UpdateProfileSettingsParams{
		ID:       params.AccountID,
		Settings: merged,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("update profile settings", err)
	}

	var out accountmodel.ProfileSettings
	_ = json.Unmarshal(updated.Settings, &out)
	return out, nil
}
