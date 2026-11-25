package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/authn"
)

func skillView(sk repository.Skill) Skill {
	v := Skill{
		ID: sk.ID, CreatedBy: sk.CreatedBy, Name: sk.Name, Slug: sk.Slug, Description: sk.Description, Prompt: sk.Prompt,
		UpdatedAt: sk.UpdatedAt,
	}
	_ = json.Unmarshal(sk.Variables, &v.Variables)
	if v.Variables == nil {
		v.Variables = []SkillVariable{}
	}
	return v
}

// Skill reads one skill.
func (s *Agent) Skill(ctx context.Context, _ authn.ConsoleMember, id uuid.UUID) (Skill, error) {
	sk, err := s.d.Runs.Skill(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return Skill{}, domain.ErrSkillNotFound
	}
	if err != nil {
		return Skill{}, fmt.Errorf("read the skill: %w", err)
	}
	return skillView(sk), nil
}

// Skills lists every skill by name; each is shared with every member.
func (s *Agent) Skills(ctx context.Context, _ authn.ConsoleMember) ([]Skill, error) {
	list, err := s.d.Runs.Skills(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the skills: %w", err)
	}
	out := make([]Skill, 0, len(list))
	for i := range list {
		out = append(out, skillView(list[i]))
	}
	return out, nil
}

// CreateSkill saves a skill made by caller; the slug is the name in
// lower-case ASCII when it has none. A key caller used answers its skill.
func (s *Agent) CreateSkill(ctx context.Context, caller authn.ConsoleMember, in SkillInput) (Skill, error) {
	if in.Key != nil {
		if v, ok, err := s.skillOfKey(ctx, caller, *in.Key); err != nil || ok {
			return v, err
		}
	}
	slug := in.Slug
	if slug == "" {
		slug = domain.SlugOf(in.Name)
	}
	name, prompt, err := domain.CheckSkill(in.Name, slug, in.Description, in.Prompt, in.Variables)
	if err != nil {
		return Skill{}, err
	}
	vars, err := json.Marshal(orEmpty(in.Variables))
	if err != nil {
		return Skill{}, fmt.Errorf("encode the variables: %w", err)
	}
	now := s.d.Now().UTC()
	sk := repository.Skill{
		ID: uuid.NewV7(), CreatedBy: caller.MemberID, Name: name, Slug: slug, Description: in.Description, Prompt: prompt,
		Variables: vars, CreatedAt: now, UpdatedAt: now, RequestKey: in.Key, WriteID: uuid.NewV7(),
	}
	err = s.d.Runs.CreateSkill(ctx, sk)
	if err != nil && in.Key != nil {
		// A racing retry of the key lost to the first; answer the first.
		if v, ok, keyErr := s.skillOfKey(ctx, caller, *in.Key); keyErr == nil && ok {
			return v, nil
		}
	}
	switch {
	case errors.Is(err, repository.ErrTaken):
		return Skill{}, domain.ErrSkillTaken
	case errors.Is(err, repository.ErrKeyUsed):
		return Skill{}, apperr.ErrTryAgain
	case err != nil:
		return Skill{}, fmt.Errorf("create the skill: %w", err)
	}
	return skillView(sk), nil
}

// skillOfKey reads the skill caller made with key, when there is one.
func (s *Agent) skillOfKey(ctx context.Context, caller authn.ConsoleMember, key uuid.UUID) (Skill, bool, error) {
	sk, err := s.d.Runs.SkillByKey(ctx, caller.MemberID, key)
	if errors.Is(err, repository.ErrNotFound) {
		return Skill{}, false, nil
	}
	if err != nil {
		return Skill{}, false, fmt.Errorf("read the key's skill: %w", err)
	}
	return skillView(sk), true, nil
}

// UpdateSkill changes the fields in sets; only its creator or an admin may.
func (s *Agent) UpdateSkill(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID, in SkillPatch) (Skill, error) {
	sk, err := s.ownSkill(ctx, caller, id)
	if err != nil {
		return Skill{}, err
	}
	var vars []SkillVariable
	_ = json.Unmarshal(sk.Variables, &vars)
	if in.Name != nil {
		sk.Name = *in.Name
	}
	if in.Slug != nil {
		sk.Slug = *in.Slug
	}
	if in.Description != nil {
		sk.Description = *in.Description
	}
	if in.Prompt != nil {
		sk.Prompt = *in.Prompt
	}
	if in.Variables != nil {
		vars = *in.Variables
	}
	if sk.Name, sk.Prompt, err = domain.CheckSkill(sk.Name, sk.Slug, sk.Description, sk.Prompt, vars); err != nil {
		return Skill{}, err
	}
	if sk.Variables, err = json.Marshal(orEmpty(vars)); err != nil {
		return Skill{}, fmt.Errorf("encode the variables: %w", err)
	}
	read := sk.WriteID
	sk.UpdatedAt, sk.WriteID = s.d.Now().UTC(), uuid.NewV7()
	switch err = s.d.Runs.UpdateSkill(ctx, sk, read); {
	case errors.Is(err, repository.ErrNotFound):
		return Skill{}, domain.ErrSkillNotFound
	case errors.Is(err, repository.ErrConflict):
		return Skill{}, apperr.ErrTryAgain
	case errors.Is(err, repository.ErrTaken):
		return Skill{}, domain.ErrSkillTaken
	case err != nil:
		return Skill{}, fmt.Errorf("update the skill: %w", err)
	}
	return skillView(sk), nil
}

// DeleteSkill deletes a skill; only its creator or an admin may.
func (s *Agent) DeleteSkill(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) error {
	if _, err := s.ownSkill(ctx, caller, id); err != nil {
		return err
	}
	if err := s.d.Runs.DeleteSkill(ctx, id); err != nil {
		return fmt.Errorf("delete the skill: %w", err)
	}
	return nil
}

// ownSkill reads a skill caller may change: their own, any for an admin.
func (s *Agent) ownSkill(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (repository.Skill, error) {
	sk, err := s.d.Runs.Skill(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return repository.Skill{}, domain.ErrSkillNotFound
	}
	if err != nil {
		return repository.Skill{}, fmt.Errorf("read the skill: %w", err)
	}
	if sk.CreatedBy != caller.MemberID && caller.Role != "admin" {
		return repository.Skill{}, domain.ErrForbidden
	}
	return sk, nil
}

// orEmpty is vars, or an empty list for none.
func orEmpty(vars []SkillVariable) []SkillVariable {
	if vars == nil {
		return []SkillVariable{}
	}
	return vars
}
