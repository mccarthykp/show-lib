package store

import (
	"context"
	"sort"
	"strings"

	"show-lib/internal/search"
)

type titleKey struct {
	media string
	id    int64
}

type titleMatch struct {
	kind  int // Title name before credited person name.
	score int
}

func (s *Store) SearchTitles(ctx context.Context, query string) ([]Title, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return s.ListTitles(ctx)
	}
	titles, err := s.ListTitles(ctx)
	if err != nil {
		return nil, err
	}
	scores := make(map[titleKey]titleMatch, len(titles))
	for _, title := range titles {
		if score := search.Score(query, title.Name); score >= 0 {
			scores[titleKey{title.MediaType, title.TMDBID}] = titleMatch{score: score}
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT t.media_type, t.tmdb_id, p.name FROM titles t
		JOIN credits c ON c.title_id = t.id JOIN people p ON p.tmdb_id = c.person_tmdb_id
		WHERE t.watched = 1 AND t.import_status = 'complete'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var media, name string
		var id int64
		if err := rows.Scan(&media, &id, &name); err != nil {
			return nil, err
		}
		score := search.Score(query, name)
		if score < 0 {
			continue
		}
		key := titleKey{media, id}
		if existing, ok := scores[key]; !ok || existing.kind == 1 && score < existing.score {
			scores[key] = titleMatch{kind: 1, score: score}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var matched []Title
	for _, title := range titles {
		if _, ok := scores[titleKey{title.MediaType, title.TMDBID}]; ok {
			matched = append(matched, title)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		a := scores[titleKey{matched[i].MediaType, matched[i].TMDBID}]
		b := scores[titleKey{matched[j].MediaType, matched[j].TMDBID}]
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		return a.score < b.score
	})
	return matched, nil
}

func (s *Store) SearchPeople(ctx context.Context, query string) ([]Person, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	return s.searchPeopleFuzzy(ctx, query)
}

func (s *Store) searchPeopleFuzzy(ctx context.Context, query string) ([]Person, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.tmdb_id, p.name, c.category,
		COALESCE(c.character_name, ''), COALESCE(c.job, ''), COALESCE(c.department, '')
		FROM people p JOIN credits c ON c.person_tmdb_id = p.tmdb_id
		JOIN titles t ON t.id = c.title_id
		WHERE t.watched = 1 AND t.import_status = 'complete'
		ORDER BY p.name COLLATE NOCASE, p.tmdb_id, c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type fuzzyPerson struct {
		person    Person
		score     int
		kind      int // name before role
		roles     []string
		seenRoles map[string]bool
	}
	const (
		nameMatch = iota
		castRoleMatch
		crewRoleMatch
	)
	matches := map[int64]*fuzzyPerson{}
	add := func(id int64, name string, score, kind int, role string) {
		if score < 0 {
			return
		}
		item := matches[id]
		if item == nil {
			item = &fuzzyPerson{person: Person{TMDBID: id, Name: name}, score: score, kind: kind,
				seenRoles: make(map[string]bool)}
			matches[id] = item
		}
		if score < item.score || score == item.score && kind < item.kind {
			item.score, item.kind, item.roles, item.seenRoles = score, kind, nil, make(map[string]bool)
		}
		if score == item.score && kind == item.kind && kind != nameMatch && role != "" && !item.seenRoles[role] {
			item.roles = append(item.roles, role)
			item.seenRoles[role] = true
		}
	}
	for rows.Next() {
		var id int64
		var name, category, character, job, department string
		if err := rows.Scan(&id, &name, &category, &character, &job, &department); err != nil {
			return nil, err
		}
		role := strings.TrimSpace(character)
		roleScore := search.Score(query, role)
		matchedRole := role
		if category == "crew" {
			roleScore = search.Score(query, job)
			if score := search.Score(query, department); score >= 0 && (roleScore < 0 || score < roleScore) {
				roleScore, matchedRole = score, strings.TrimSpace(department)
			} else {
				matchedRole = strings.TrimSpace(job)
			}
		}
		roleKind := castRoleMatch
		if category == "crew" {
			roleKind = crewRoleMatch
		}
		add(id, name, search.Score(query, name), nameMatch, "")
		add(id, name, roleScore, roleKind, matchedRole)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]*fuzzyPerson, 0, len(matches))
	for _, item := range matches {
		item.person.Roles = strings.Join(item.roles, ", ")
		if item.kind == castRoleMatch {
			item.person.MatchLabel = "Role"
		} else if item.kind == crewRoleMatch {
			item.person.MatchLabel = "Crew"
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score < items[j].score
		}
		return strings.ToLower(items[i].person.Name) < strings.ToLower(items[j].person.Name)
	})
	people := make([]Person, 0, len(items))
	for _, item := range items {
		people = append(people, item.person)
	}
	return people, nil
}
