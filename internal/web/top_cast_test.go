package web

import (
	"database/sql"
	"testing"

	"show-lib/internal/store"
)

func TestRankTopCast(t *testing.T) {
	credits := []store.CreditView{
		{PersonTMDBID: 1, PersonName: "Second", Category: "cast", BillingOrder: sql.NullInt64{Int64: 3, Valid: true}, Episodes: []store.CreditEpisode{{ID: 11}, {ID: 12}}},
		{PersonTMDBID: 2, PersonName: "First", Category: "cast", BillingOrder: sql.NullInt64{Int64: 5, Valid: true}, Episodes: []store.CreditEpisode{{ID: 21}, {ID: 22}}},
		{PersonTMDBID: 2, PersonName: "First", Category: "cast", BillingOrder: sql.NullInt64{Int64: 5, Valid: true}, Episodes: []store.CreditEpisode{{ID: 22}, {ID: 23}}},
		{PersonTMDBID: 3, PersonName: "Third", Category: "cast", BillingOrder: sql.NullInt64{Int64: 0, Valid: true}},
		{PersonTMDBID: 4, PersonName: "Crew", Category: "crew", BillingOrder: sql.NullInt64{Int64: 0, Valid: true}},
		{PersonTMDBID: 5, PersonName: "No Portrait", Category: "cast", BillingOrder: sql.NullInt64{Int64: 1, Valid: true}},
	}
	for _, tc := range []struct {
		media string
		want  []int64
	}{
		{media: "tv", want: []int64{2, 1, 3, 5}},
		{media: "movie", want: []int64{3, 5, 1, 2}},
	} {
		got := rankTopCast(credits, tc.media)
		if len(got) != len(tc.want) {
			t.Fatalf("%s cast length = %d, want %d", tc.media, len(got), len(tc.want))
		}
		for i, id := range tc.want {
			if got[i].ID != id {
				t.Errorf("%s cast at %d = %d, want %d", tc.media, i, got[i].ID, id)
			}
		}
	}
}

func TestRankTopCastLimitsResults(t *testing.T) {
	credits := make([]store.CreditView, 0, topCastLimit+5)
	for i := 0; i < topCastLimit+5; i++ {
		credits = append(credits, store.CreditView{
			PersonTMDBID: int64(i + 1), PersonName: "Cast member",
			Category: "cast", BillingOrder: sql.NullInt64{Int64: int64(i), Valid: true},
		})
	}
	ranked := rankTopCast(credits, "movie")
	if len(ranked) != topCastLimit || ranked[len(ranked)-1].ID != int64(topCastLimit) {
		t.Fatalf("limited cast = %d entries ending with %d", len(ranked), ranked[len(ranked)-1].ID)
	}
}
