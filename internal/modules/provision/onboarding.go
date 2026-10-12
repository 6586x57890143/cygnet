package provision

import (
	"slices"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
)

// Onboarding is what Discord shows someone joining: the channels they're in
// from the start, and the questions whose answers open more. It runs in
// advanced mode, where Discord counts the answers' channels with the
// defaults toward what onboarding needs (7 channels, 5 of them open to
// talk in).
type Onboarding struct {
	Defaults  []string
	Questions []Question
}

// Question is one onboarding question; each answer opens its channels.
type Question struct {
	Title    string
	Multi    bool
	Required bool
	Answers  []Answer
}

type Answer struct {
	Emoji, Title string
	Channels     []string
}

// onboardingDiff is what about the server's onboarding differs from l's:
// on, the mode, the default channels, each question by its title.
func (s state) onboardingDiff(l Layout) []string {
	o := s.onboarding
	if o == nil {
		return []string{"on"}
	}
	var out []string
	if !o.Enabled {
		out = append(out, "on")
	}
	if o.Mode != discord.GuildOnboardingModeAdvanced {
		out = append(out, "mode")
	}
	if want, ok := s.channelIDs(l.Onboarding.Defaults); !ok || !sameIDs(want, o.DefaultChannelIDs) {
		out = append(out, "channels")
	}
	for _, q := range l.Onboarding.Questions {
		if !s.asked(q, o.Prompts) {
			out = append(out, "question")
			break
		}
	}
	return out
}

// asked is whether prompts holds q as the layout asks it.
func (s state) asked(q Question, prompts []discord.GuildOnboardingPrompt) bool {
	i := slices.IndexFunc(prompts, func(p discord.GuildOnboardingPrompt) bool { return strings.EqualFold(p.Title, q.Title) })
	if i < 0 {
		return false
	}
	p := prompts[i]
	if p.SingleSelect == q.Multi || p.Required != q.Required || !p.InOnboarding || len(p.Options) != len(q.Answers) {
		return false
	}
	for j, a := range q.Answers {
		want, ok := s.channelIDs(a.Channels)
		if !ok || !strings.EqualFold(p.Options[j].Title, a.Title) || !sameIDs(want, p.Options[j].ChannelIDs) {
			return false
		}
	}
	return true
}

// channelIDs is each named channel's ID, false if one isn't here yet.
func (s state) channelIDs(names []string) ([]snowflake.ID, bool) {
	var out []snowflake.ID
	for _, name := range names {
		i := slices.IndexFunc(s.channels, func(c discord.GuildChannel) bool {
			return c.Type() != discord.ChannelTypeGuildCategory && strings.EqualFold(bare(c.Name()), name)
		})
		if i < 0 {
			return nil, false
		}
		out = append(out, s.channels[i].ID())
	}
	return out, true
}

func sameIDs(a, b []snowflake.ID) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// Onboard is what writing onboarding calls on Discord. Channels are read
// again at the write, so ones the same apply made have their IDs.
type Onboard interface {
	GetGuildChannels(guildID snowflake.ID, opts ...rest.RequestOpt) ([]discord.GuildChannel, error)
	GetGuildOnboarding(guildID snowflake.ID, opts ...rest.RequestOpt) (*discord.GuildOnboarding, error)
	UpdateGuildOnboarding(guildID snowflake.ID, onboardingUpdate discord.GuildOnboardingUpdate, opts ...rest.RequestOpt) (*discord.GuildOnboarding, error)
}

// onboard writes l's onboarding: its questions in place of any with their
// titles, after the questions an admin added, which are kept.
func onboard(guild snowflake.ID, w Onboard, opts []rest.RequestOpt, l Layout) error {
	channels, err := w.GetGuildChannels(guild, opts...)
	if err != nil {
		return err
	}
	have, err := w.GetGuildOnboarding(guild, opts...)
	if err != nil {
		return err
	}
	s := state{guild: guild, channels: channels}
	defaults, _ := s.channelIDs(l.Onboarding.Defaults)
	var prompts []discord.GuildOnboardingPrompt
	for _, p := range have.Prompts {
		if !slices.ContainsFunc(l.Onboarding.Questions, func(q Question) bool { return strings.EqualFold(q.Title, p.Title) }) {
			prompts = append(prompts, p)
		}
	}
	for _, q := range l.Onboarding.Questions {
		// A question and its answers keep the IDs they have, so members'
		// answers outlast a reapply.
		var old discord.GuildOnboardingPrompt
		if i := slices.IndexFunc(have.Prompts, func(p discord.GuildOnboardingPrompt) bool { return strings.EqualFold(p.Title, q.Title) }); i >= 0 {
			old = have.Prompts[i]
		}
		p := discord.GuildOnboardingPrompt{ID: idOr(old.ID), Type: discord.GuildOnboardingPromptTypeMultipleChoice, Title: q.Title, SingleSelect: !q.Multi, Required: q.Required, InOnboarding: true}
		for _, a := range q.Answers {
			ids, _ := s.channelIDs(a.Channels)
			var was snowflake.ID
			if i := slices.IndexFunc(old.Options, func(o discord.GuildOnboardingPromptOption) bool { return strings.EqualFold(o.Title, a.Title) }); i >= 0 {
				was = old.Options[i].ID
			}
			p.Options = append(p.Options, discord.GuildOnboardingPromptOption{ID: idOr(was), Title: a.Title, ChannelIDs: ids, RoleIDs: []snowflake.ID{}, Emoji: discord.PartialEmoji{Name: &a.Emoji}})
		}
		prompts = append(prompts, p)
	}
	on, mode := true, discord.GuildOnboardingModeAdvanced
	_, err = w.UpdateGuildOnboarding(guild, discord.GuildOnboardingUpdate{Prompts: &prompts, DefaultChannelIDs: &defaults, Enabled: &on, Mode: &mode}, opts...)
	return err
}

// idOr is id, or a fresh one for a new question or answer, which Discord
// takes from whoever writes it.
func idOr(id snowflake.ID) snowflake.ID {
	if id != 0 {
		return id
	}
	return snowflake.New(time.Now())
}
