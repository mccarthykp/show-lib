# Phase 4 — People and connections

**Status:** Complete. The local Docker service is available at `http://127.0.0.1:8080`.

## Delivered

- The Titles page searches saved titles by title or credited person's name. The People page searches by person, saved title, character, crew job, or department. Search matches typed characters in order while ignoring spaces and dashes. Name matches on the People page show only the real name; role matches show only the matching role. The Add page tries shorter TMDB queries when a search returns no relevant match, then filters and ranks candidates by the same ordered matching rule. Title pages filter their full cast and crew by person, character, or job.
- Results on Add, Titles, People, and the title-page cast/crew filter update as the user types. Requests begin after a brief pause; older requests are canceled so their results cannot replace a newer query. Search forms still work without JavaScript.
- A collapsed connection panel sits directly beneath each completed title's heading and reads "N connections," counting each shared person-to-title link. Expanding it links watched-range shared people to other saved titles, shows the person's role or job on both titles, and offers expandable episode evidence on either side. Multiple people who connect to one other title appear on one comma-separated line. A person connecting to several titles gets a separate line listing those titles. With no connections, the page shows only "No connections" as primary text.
- Connections are derived on each title request from saved credits and the current watch markers. Future episodes, unmapped aggregate credits, and series whose episode credits are still unavailable cannot establish a connection. Adding or removing a title, changing progress, or refreshing credits changes the panel without a separate graph cache.

## Verification

- `go test ./...` and `go vet ./...` pass. Web tests cover shared series credits, a movie-to-series connection, multiple people and titles, watched-progress advance and rollback, unmapped credits, removal, and re-addition. Search tests cover ordered characters, ignored spaces and dashes, rejected transpositions, and Add-page candidate filtering.
- The preserved local library still contains *The Sopranos* and *Nurse Jackie*. Both title pages return successfully and show mutual connections. Search results update without submitting the form.
