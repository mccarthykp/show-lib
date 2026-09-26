package web

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"show-lib/internal/store"
)

type connectionPerson struct {
	ID   int64
	Name string
}

type connectionTitle struct {
	MediaType string
	ID        int64
	Name      string
}

type connectionDetail struct {
	Person        connectionPerson
	Other         connectionTitle
	HereRoles     string
	OtherRoles    string
	HereEpisodes  []store.CreditEpisode
	OtherEpisodes []store.CreditEpisode
}

type connectionGroup struct {
	ByPerson bool
	People   []connectionPerson
	Titles   []connectionTitle
	Details  []connectionDetail
}

type watchedCredit struct {
	person   connectionPerson
	roles    []string
	episodes []store.CreditEpisode
	roleSeen map[string]bool
	epSeen   map[int64]bool
}

func watchedCredits(credits []store.CreditView) map[int64]*watchedCredit {
	result := make(map[int64]*watchedCredit)
	for _, credit := range credits {
		if credit.Status != "watched" {
			continue
		}
		item := result[credit.PersonTMDBID]
		if item == nil {
			item = &watchedCredit{person: connectionPerson{ID: credit.PersonTMDBID, Name: credit.PersonName},
				roleSeen: make(map[string]bool), epSeen: make(map[int64]bool)}
			result[credit.PersonTMDBID] = item
		}
		role := strings.TrimSpace(credit.CharacterName)
		if credit.Category == "crew" {
			role = strings.TrimSpace(credit.Job)
			if role == "" {
				role = strings.TrimSpace(credit.Department)
			}
		}
		if role == "" {
			if credit.Category == "cast" {
				role = "Cast"
			} else {
				role = "Crew"
			}
		}
		if !item.roleSeen[role] {
			item.roles = append(item.roles, role)
			item.roleSeen[role] = true
		}
		for _, ep := range credit.Episodes {
			if ep.Watched && !item.epSeen[ep.ID] {
				item.episodes = append(item.episodes, ep)
				item.epSeen[ep.ID] = true
			}
		}
	}
	for _, item := range result {
		sort.Slice(item.episodes, func(i, j int) bool {
			a, b := item.episodes[i], item.episodes[j]
			if a.Season != b.Season {
				return a.Season < b.Season
			}
			return a.Number < b.Number
		})
	}
	return result
}

func (s *Server) connections(ctx context.Context, current store.Title, currentCredits []store.CreditView) ([]connectionGroup, error) {
	here := watchedCredits(currentCredits)
	if len(here) == 0 {
		return nil, nil
	}
	titles, err := s.store.ListTitles(ctx)
	if err != nil {
		return nil, err
	}
	byPerson := make(map[int64][]connectionDetail)
	byTitle := make(map[string][]connectionDetail)
	for _, title := range titles {
		if title.MediaType == current.MediaType && title.TMDBID == current.TMDBID {
			continue
		}
		credits, err := s.store.TitleCredits(ctx, title.MediaType, title.TMDBID)
		if err != nil {
			return nil, err
		}
		if err := s.store.ApplyCreditAppearances(ctx, title, credits); err != nil {
			return nil, err
		}
		other := watchedCredits(credits)
		for personID, a := range here {
			b := other[personID]
			if b == nil {
				continue
			}
			detail := connectionDetail{Person: a.person,
				Other:     connectionTitle{MediaType: title.MediaType, ID: title.TMDBID, Name: title.Name},
				HereRoles: strings.Join(a.roles, ", "), OtherRoles: strings.Join(b.roles, ", "),
				HereEpisodes: a.episodes, OtherEpisodes: b.episodes}
			byPerson[personID] = append(byPerson[personID], detail)
			key := fmt.Sprintf("%s:%d", title.MediaType, title.TMDBID)
			byTitle[key] = append(byTitle[key], detail)
		}
	}
	var groups []connectionGroup
	for _, details := range byTitle {
		var singles []connectionDetail
		for _, detail := range details {
			if len(byPerson[detail.Person.ID]) == 1 {
				singles = append(singles, detail)
			}
		}
		if len(singles) == 0 {
			continue
		}
		sort.Slice(singles, func(i, j int) bool { return singles[i].Person.Name < singles[j].Person.Name })
		group := connectionGroup{Titles: []connectionTitle{singles[0].Other}, Details: singles}
		for _, detail := range singles {
			group.People = append(group.People, detail.Person)
		}
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Titles[0].Name < groups[j].Titles[0].Name })
	var multi []connectionGroup
	for _, details := range byPerson {
		if len(details) < 2 {
			continue
		}
		sort.Slice(details, func(i, j int) bool { return details[i].Other.Name < details[j].Other.Name })
		group := connectionGroup{ByPerson: true, People: []connectionPerson{details[0].Person}, Details: details}
		for _, detail := range details {
			group.Titles = append(group.Titles, detail.Other)
		}
		multi = append(multi, group)
	}
	sort.Slice(multi, func(i, j int) bool { return multi[i].People[0].Name < multi[j].People[0].Name })
	return append(groups, multi...), nil
}
