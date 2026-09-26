package web

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"show-lib/internal/store"
)

func personTitleRoute(r *http.Request) (int64, string, int64, bool) {
	personID, personOK := positiveID(r.PathValue("id"))
	media := r.PathValue("media")
	titleID, titleOK := positiveID(r.PathValue("titleID"))
	return personID, media, titleID, personOK && titleOK && (media == "movie" || media == "tv")
}

func (s *Server) personTitleForRemoval(r *http.Request) (store.Person, store.Title, bool, error) {
	personID, media, titleID, ok := personTitleRoute(r)
	if !ok {
		return store.Person{}, store.Title{}, false, nil
	}
	person, err := s.store.GetPerson(r.Context(), personID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Person{}, store.Title{}, false, nil
	}
	if err != nil {
		return store.Person{}, store.Title{}, false, err
	}
	titles, err := s.store.PersonTitles(r.Context(), personID)
	if err != nil {
		return store.Person{}, store.Title{}, false, err
	}
	for _, candidate := range titles {
		if candidate.MediaType == media && candidate.TMDBID == titleID {
			title, err := s.store.GetTitle(r.Context(), media, titleID)
			if errors.Is(err, sql.ErrNoRows) {
				return store.Person{}, store.Title{}, false, nil
			}
			return person, title, err == nil, err
		}
	}
	return store.Person{}, store.Title{}, false, nil
}

func (s *Server) removePersonTitleConfirm(w http.ResponseWriter, r *http.Request) {
	person, title, found, err := s.personTitleForRemoval(r)
	if err != nil {
		http.Error(w, "could not load title", http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	s.render(w, pageData{Heading: "Remove " + title.Name + "?", Kind: "remove-person", Person: person, Title: title})
}

func (s *Server) removePersonTitle(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	person, title, found, err := s.personTitleForRemoval(r)
	if err != nil {
		http.Error(w, "could not load title", http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteTitle(r.Context(), title.MediaType, title.TMDBID); err != nil {
		http.Error(w, "could not remove title", http.StatusInternalServerError)
		return
	}
	titles, err := s.store.PersonTitles(r.Context(), person.TMDBID)
	if err != nil {
		http.Error(w, "could not load person credits", http.StatusInternalServerError)
		return
	}
	if len(titles) == 0 {
		http.Redirect(w, r, "/titles", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/people/%d", person.TMDBID), http.StatusSeeOther)
}
