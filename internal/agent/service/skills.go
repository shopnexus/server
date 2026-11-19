package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
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
	sk, err := s.runs.Skill(ctx, id)
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
	list, err := s.runs.Skills(ctx)
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
// lower-case ASCII when it has none.
func (s *Agent) CreateSkill(ctx context.Context, caller authn.ConsoleMember, in SkillInput) (Skill, error) {
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
	now := s.now().UTC()
	sk := repository.Skill{
		ID: uuid.NewV7(), CreatedBy: caller.MemberID, Name: name, Slug: slug, Description: in.Description, Prompt: prompt,
		Variables: vars, CreatedAt: now, UpdatedAt: now,
	}
	if err = s.saveSkill(ctx, sk); err != nil {
		return Skill{}, err
	}
	return skillView(sk), nil
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
	sk.UpdatedAt = s.now().UTC()
	if err = s.saveSkill(ctx, sk); err != nil {
		return Skill{}, err
	}
	return skillView(sk), nil
}

// DeleteSkill deletes a skill; only its creator or an admin may.
func (s *Agent) DeleteSkill(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) error {
	if _, err := s.ownSkill(ctx, caller, id); err != nil {
		return err
	}
	if err := s.runs.DeleteSkill(ctx, id); err != nil {
		return fmt.Errorf("delete the skill: %w", err)
	}
	return nil
}

// ownSkill reads a skill caller may change: their own, any for an admin.
func (s *Agent) ownSkill(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (repository.Skill, error) {
	sk, err := s.runs.Skill(ctx, id)
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

func (s *Agent) saveSkill(ctx context.Context, sk repository.Skill) error {
	err := s.runs.SaveSkill(ctx, sk)
	if errors.Is(err, repository.ErrTaken) {
		return domain.ErrSkillTaken
	}
	if err != nil {
		return fmt.Errorf("save the skill: %w", err)
	}
	return nil
}

// orEmpty is vars, or an empty list for none.
func orEmpty(vars []SkillVariable) []SkillVariable {
	if vars == nil {
		return []SkillVariable{}
	}
	return vars
}
