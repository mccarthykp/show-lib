package web

import (
	"sort"
	"strings"

	"show-lib/internal/store"
)

type topCastPerson struct {
	ID              int64
	Name            string
	EpisodeCount    int
	BillingOrder    int64
	HasBillingOrder bool
}

const topCastLimit = 20

func rankTopCast(credits []store.CreditView, media string) []topCastPerson {
	people := make(map[int64]*topCastPerson)
	episodes := make(map[int64]map[int64]struct{})
	for _, credit := range credits {
		if credit.Category != "cast" || credit.PersonTMDBID <= 0 {
			continue
		}
		person := people[credit.PersonTMDBID]
		if person == nil {
			person = &topCastPerson{ID: credit.PersonTMDBID, Name: strings.TrimSpace(credit.PersonName)}
			people[credit.PersonTMDBID] = person
		}
		if credit.BillingOrder.Valid && (!person.HasBillingOrder || credit.BillingOrder.Int64 < person.BillingOrder) {
			person.BillingOrder = credit.BillingOrder.Int64
			person.HasBillingOrder = true
		}
		if episodes[credit.PersonTMDBID] == nil {
			episodes[credit.PersonTMDBID] = make(map[int64]struct{})
		}
		for _, episode := range credit.Episodes {
			episodes[credit.PersonTMDBID][episode.ID] = struct{}{}
		}
	}
	ranked := make([]topCastPerson, 0, len(people))
	for id, person := range people {
		person.EpisodeCount = len(episodes[id])
		ranked = append(ranked, *person)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if media == "tv" && ranked[i].EpisodeCount != ranked[j].EpisodeCount {
			return ranked[i].EpisodeCount > ranked[j].EpisodeCount
		}
		if ranked[i].HasBillingOrder != ranked[j].HasBillingOrder {
			return ranked[i].HasBillingOrder
		}
		if ranked[i].HasBillingOrder && ranked[i].BillingOrder != ranked[j].BillingOrder {
			return ranked[i].BillingOrder < ranked[j].BillingOrder
		}
		if !strings.EqualFold(ranked[i].Name, ranked[j].Name) {
			return strings.ToLower(ranked[i].Name) < strings.ToLower(ranked[j].Name)
		}
		return ranked[i].ID < ranked[j].ID
	})
	if len(ranked) > topCastLimit {
		ranked = ranked[:topCastLimit]
	}
	return ranked
}
