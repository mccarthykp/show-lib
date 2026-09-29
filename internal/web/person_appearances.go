package web

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"show-lib/internal/store"
	"show-lib/internal/tmdb"
)

type personAppearance struct {
	store.PersonTitle
	PersonID    int64
	InWatchlist bool
}

func groupPersonAppearances(personID int64, saved []store.PersonTitle, credits tmdb.PersonCombinedCredits) (watched, unwatched []personAppearance) {
	savedIndex := make(map[string]int, len(saved))
	for _, title := range saved {
		savedIndex[appearanceKey(title.MediaType, title.TMDBID)] = len(watched)
		appearance := personAppearance{PersonTitle: title, PersonID: personID}
		watched = append(watched, appearance)
	}

	var remote []store.PersonTitle
	remoteIndex := make(map[string]int)
	roleSeen := make(map[string]map[string]bool)
	addCredit := func(credit tmdb.PersonCredit, role string) {
		if credit.ID <= 0 || credit.MediaType != "movie" && credit.MediaType != "tv" {
			return
		}
		name, date := strings.TrimSpace(credit.Title), credit.ReleaseDate
		if credit.MediaType == "tv" {
			name, date = strings.TrimSpace(credit.Name), credit.FirstAirDate
		}
		key := appearanceKey(credit.MediaType, credit.ID)
		if index, ok := savedIndex[key]; ok {
			if watched[index].Overview == "" {
				watched[index].Overview = strings.TrimSpace(credit.Overview)
			}
			if len(date) > len(watched[index].ReleaseSort) {
				watched[index].ReleaseSort = date
			}
			return
		}
		if name == "" {
			return
		}
		index, ok := remoteIndex[key]
		if !ok {
			index = len(remote)
			remoteIndex[key] = index
			roleSeen[key] = make(map[string]bool)
			remote = append(remote, store.PersonTitle{
				MediaType:     credit.MediaType,
				TMDBID:        credit.ID,
				Name:          name,
				PosterPath:    sql.NullString{String: credit.PosterPath, Valid: credit.PosterPath != ""},
				Overview:      strings.TrimSpace(credit.Overview),
				Period:        firstYear(date),
				ReleaseSort:   date,
				WatchedStatus: "unwatched",
			})
		}
		role = strings.TrimSpace(role)
		if role == "" || roleSeen[key][role] {
			return
		}
		roleSeen[key][role] = true
		if remote[index].Roles != "" {
			remote[index].Roles += ", "
		}
		remote[index].Roles += role
	}
	for _, credit := range credits.Cast {
		addCredit(credit, credit.Character)
	}
	for _, credit := range credits.Crew {
		role := credit.Job
		if strings.TrimSpace(role) == "" {
			role = credit.Department
		}
		addCredit(credit, role)
	}
	for _, title := range remote {
		unwatched = append(unwatched, personAppearance{PersonTitle: title, PersonID: personID})
	}
	sortAppearances(watched)
	sortAppearances(unwatched)
	return watched, unwatched
}

func markWatchlisted(items []personAppearance, watchlist []store.WatchlistTitle) {
	watchlisted := make(map[string]bool, len(watchlist))
	for _, title := range watchlist {
		watchlisted[appearanceKey(title.MediaType, title.TMDBID)] = true
	}
	for i := range items {
		items[i].InWatchlist = watchlisted[appearanceKey(items[i].MediaType, items[i].TMDBID)]
	}
}

func formatReleaseDate(value string) string {
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.Format("January 2, 2006")
	}
	return firstYear(value)
}

func appearanceKey(media string, id int64) string {
	return fmt.Sprintf("%s:%d", media, id)
}

func sortAppearances(items []personAppearance) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].ReleaseSort != items[j].ReleaseSort {
			return items[i].ReleaseSort > items[j].ReleaseSort
		}
		left, right := strings.ToLower(items[i].Name), strings.ToLower(items[j].Name)
		if left != right {
			return left < right
		}
		if items[i].MediaType != items[j].MediaType {
			return items[i].MediaType < items[j].MediaType
		}
		return items[i].TMDBID < items[j].TMDBID
	})
}

func mergePersonAppearances(watched, unwatched []personAppearance) []personAppearance {
	items := make([]personAppearance, 0, len(watched)+len(unwatched))
	items = append(items, watched...)
	items = append(items, unwatched...)
	sortAppearances(items)
	return items
}
