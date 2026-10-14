package jarvis

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Voice-library request shapes were checked against the ElevenLabs API reference on 2026-10-14:
// GET /v1/shared-voices (query: page_size ≤ 100, language, locale, accent, gender, age, use_cases[], featured, sort =
// created_date | usage_character_count_1y | trending | cloned_by_count; response voices[] with public_owner_id,
// voice_id, name, accent, gender, age, descriptive, use_case, language, locale, description, preview_url, featured,
// is_added_by_user, usage_character_count_1y, verified_languages[{language, accent, locale, preview_url}]) and
// POST /v1/voices/add/{public_user_id}/{voice_id} (body new_name, response voice_id).

// VoiceGroup is one titled block of voices in the voice picker.
type VoiceGroup struct {
	Label  string  `json:"label"`
	Voices []Voice `json:"voices"`
}

// VoiceList is the jarvis.voices result: voices suited to one call language, grouped, plus the account's own library
// (used to name a stored or default voice that is not in the groups).
type VoiceList struct {
	Language string       `json:"language"` // the call language the groups are for: "nl" or "en"
	Groups   []VoiceGroup `json:"groups"`
	Library  []Voice      `json:"library"`
}

// Voice groups.
const (
	groupStandardDutch = "Standaard Nederlands"
	groupFlemish       = "Vlaams"
	groupOtherDutch    = "Andere"
	groupBestForCalls  = "Best for calls"
)

const (
	dutchPerGroup   = 12
	englishShared   = 15
	voiceCacheTTL   = 10 * time.Minute
	popularitySort  = "usage_character_count_1y"
	conversational  = "conversational"
	maxVoiceIDBytes = 64
)

// sharedVoice is a Voice Library entry with the fields used for grouping and ranking.
type sharedVoice struct {
	Voice
	Locale   string
	UseCase  string
	Featured bool
	Usage    int64
}

// libraryVoice is one of the account's own voices.
type libraryVoice struct {
	Voice
	Category string
	UseCase  string
}

type sharedQuery struct {
	Language, Locale, UseCase string
	PageSize                  int
}

// sharedVoices queries ElevenLabs' Voice Library, most used first. Accent, locale and preview come from the voice's
// verified entry for the requested language when it has one (a multilingual voice's own accent is its first language).
func (e *ElevenLabs) sharedVoices(ctx context.Context, q sharedQuery) ([]sharedVoice, error) {
	v := url.Values{}
	v.Set("page_size", strconv.Itoa(q.PageSize))
	v.Set("sort", popularitySort)
	if q.Language != "" {
		v.Set("language", q.Language)
	}
	if q.Locale != "" {
		v.Set("locale", q.Locale)
	}
	if q.UseCase != "" {
		v.Add("use_cases", q.UseCase)
	}
	var res struct {
		Voices []struct {
			PublicOwnerID string `json:"public_owner_id"`
			VoiceID       string `json:"voice_id"`
			Name          string `json:"name"`
			Accent        string `json:"accent"`
			Gender        string `json:"gender"`
			Age           string `json:"age"`
			Descriptive   string `json:"descriptive"`
			UseCase       string `json:"use_case"`
			Language      string `json:"language"`
			Locale        string `json:"locale"`
			Description   string `json:"description"`
			PreviewURL    string `json:"preview_url"`
			Featured      bool   `json:"featured"`
			Added         bool   `json:"is_added_by_user"`
			Usage         int64  `json:"usage_character_count_1y"`
			Verified      []struct {
				Language   string `json:"language"`
				Accent     string `json:"accent"`
				Locale     string `json:"locale"`
				PreviewURL string `json:"preview_url"`
			} `json:"verified_languages"`
		} `json:"voices"`
	}
	if err := e.do(ctx, "GET", "/v1/shared-voices?"+v.Encode(), nil, &res); err != nil {
		return nil, err
	}
	out := make([]sharedVoice, 0, len(res.Voices))
	for _, r := range res.Voices {
		if r.VoiceID == "" {
			continue
		}
		accent, locale, preview := r.Accent, r.Locale, r.PreviewURL
		for _, vl := range r.Verified {
			if q.Language != "" && strings.EqualFold(vl.Language, q.Language) {
				accent, locale, preview = or2(strings.TrimSpace(vl.Accent), accent), or2(vl.Locale, locale), or2(vl.PreviewURL, preview)
				break
			}
		}
		labels := map[string]string{"descriptive": r.Descriptive, "gender": r.Gender, "age": r.Age, "accent": accent, "use_case": r.UseCase}
		desc := voiceDescription("", labels)
		if d := strings.TrimSpace(r.Description); d != "" {
			if desc != "" {
				desc += ". "
			}
			desc += d
		}
		out = append(out, sharedVoice{
			Voice: Voice{ID: r.VoiceID, PublicOwnerID: r.PublicOwnerID, Name: strings.TrimSpace(r.Name), Description: desc,
				Accent: accent, Gender: r.Gender, Age: r.Age, PreviewURL: preview, InLibrary: r.Added},
			Locale: locale, UseCase: r.UseCase, Featured: r.Featured, Usage: r.Usage,
		})
	}
	return out, nil
}

// addSharedVoice copies a Voice Library voice into the account's library and returns its id there.
func (e *ElevenLabs) addSharedVoice(ctx context.Context, publicOwnerID, voiceID, name string) (string, error) {
	var res struct {
		VoiceID string `json:"voice_id"`
	}
	path := "/v1/voices/add/" + url.PathEscape(publicOwnerID) + "/" + url.PathEscape(voiceID)
	if err := e.do(ctx, "POST", path, map[string]any{"new_name": name}, &res); err != nil {
		return "", err
	}
	if res.VoiceID == "" {
		return voiceID, nil
	}
	return res.VoiceID, nil
}

// dutchGroup sorts a Dutch voice by accent: the locale when ElevenLabs gives one (nl-BE is Flemish), else the accent
// text. A Dutch voice with no accent is standard Dutch.
func dutchGroup(v sharedVoice) string {
	loc, acc := strings.ToLower(v.Locale), strings.ToLower(strings.TrimSpace(v.Accent))
	for _, w := range []string{"flem", "belg", "vla"} {
		if strings.Contains(acc, w) {
			return groupFlemish
		}
	}
	if strings.HasSuffix(loc, "-be") {
		return groupFlemish
	}
	if acc == "" || strings.HasSuffix(loc, "-nl") {
		return groupStandardDutch
	}
	for _, w := range []string{"standard", "standaard", "netherl", "nederl", "dutch", "holland", "neutral", "general"} {
		if strings.Contains(acc, w) {
			return groupStandardDutch
		}
	}
	return groupOtherDutch
}

// rankForCalls puts conversational voices first, then featured ones, then the most used (stable otherwise).
func rankForCalls(vs []sharedVoice) {
	sort.SliceStable(vs, func(i, j int) bool {
		ci, cj := strings.Contains(vs[i].UseCase, conversational), strings.Contains(vs[j].UseCase, conversational)
		if ci != cj {
			return ci
		}
		if vs[i].Featured != vs[j].Featured {
			return vs[i].Featured
		}
		return vs[i].Usage > vs[j].Usage
	})
}

func plainVoices(vs []sharedVoice, limit int, own map[string]bool) []Voice {
	out := []Voice{}
	for _, v := range vs {
		if len(out) == limit {
			break
		}
		if own[v.ID] {
			v.InLibrary = true
		}
		out = append(out, v.Voice)
	}
	return out
}

// voiceListLanguage is the voice language for a call language: Auto starts in Dutch.
func voiceListLanguage(lang string) string {
	if lang == LangEnglish {
		return LangEnglish
	}
	return LangDutch
}

// buildVoiceList fetches the voices for one language: Dutch voices grouped by accent, or English conversational voices
// plus the account's own premade conversational voices. Only the main shared query and the account library are
// required; the Flemish top-up query is best effort.
func buildVoiceList(ctx context.Context, el *ElevenLabs, lang string) (VoiceList, error) {
	lib, err := el.libraryVoices(ctx)
	if err != nil {
		return VoiceList{}, err
	}
	own := map[string]bool{}
	library := make([]Voice, 0, len(lib))
	for _, v := range lib {
		own[v.ID] = true
		library = append(library, v.Voice)
	}
	list := VoiceList{Language: lang, Library: library, Groups: []VoiceGroup{}}

	if lang == LangEnglish {
		shared, err := el.sharedVoices(ctx, sharedQuery{Language: LangEnglish, UseCase: conversational, PageSize: 40})
		if err != nil {
			return VoiceList{}, err
		}
		voices := []Voice{}
		for _, v := range lib {
			if v.Category == "premade" && strings.Contains(strings.ToLower(v.UseCase), conversational) {
				voices = append(voices, v.Voice)
			}
		}
		sort.SliceStable(shared, func(i, j int) bool { return shared[i].Featured && !shared[j].Featured })
		seen := map[string]bool{}
		for _, v := range voices {
			seen[v.ID] = true
		}
		var rest []sharedVoice
		for _, v := range shared {
			if !seen[v.ID] {
				seen[v.ID] = true
				rest = append(rest, v)
			}
		}
		voices = append(voices, plainVoices(rest, englishShared, own)...)
		list.Groups = append(list.Groups, VoiceGroup{Label: groupBestForCalls, Voices: voices})
		return list, nil
	}

	shared, err := el.sharedVoices(ctx, sharedQuery{Language: LangDutch, PageSize: 100})
	if err != nil {
		return VoiceList{}, err
	}
	// The most used Dutch voices are mostly from the Netherlands; ask for Belgian ones separately so Flemish shows.
	if be, err := el.sharedVoices(ctx, sharedQuery{Language: LangDutch, Locale: "nl-BE", PageSize: 50}); err == nil {
		shared = append(shared, be...)
	}
	seen := map[string]bool{}
	byGroup := map[string][]sharedVoice{}
	for _, v := range shared {
		if seen[v.ID] {
			continue
		}
		seen[v.ID] = true
		g := dutchGroup(v)
		byGroup[g] = append(byGroup[g], v)
	}
	for _, g := range []string{groupStandardDutch, groupFlemish, groupOtherDutch} {
		vs := byGroup[g]
		if len(vs) == 0 {
			continue
		}
		rankForCalls(vs)
		list.Groups = append(list.Groups, VoiceGroup{Label: g, Voices: plainVoices(vs, dutchPerGroup, own)})
	}
	return list, nil
}

// voiceCache keeps voice lists per language for voiceCacheTTL.
type voiceCache struct {
	mu      sync.Mutex
	entries map[string]voiceCacheEntry
}

type voiceCacheEntry struct {
	list VoiceList
	at   time.Time
}

func (c *voiceCache) get(lang string, now time.Time) (VoiceList, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[lang]
	if !ok || now.Sub(e.at) > voiceCacheTTL {
		return VoiceList{}, false
	}
	return e.list, true
}

func (c *voiceCache) put(lang string, l VoiceList, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]voiceCacheEntry{}
	}
	c.entries[lang] = voiceCacheEntry{list: l, at: now}
}

func (c *voiceCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = nil
}

// find returns a cached voice by id, from any language.
func (c *voiceCache) find(id string) (Voice, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.entries {
		for _, v := range e.list.Library {
			if v.ID == id {
				return v, true
			}
		}
		for _, g := range e.list.Groups {
			for _, v := range g.Voices {
				if v.ID == id {
					return v, true
				}
			}
		}
	}
	return Voice{}, false
}

// VoiceList returns the voices for a call language ("" = the chosen call language; Auto lists Dutch voices), cached
// for ten minutes per language.
func (s *Service) VoiceList(ctx context.Context, lang string) (VoiceList, error) {
	switch lang {
	case "":
		lang = s.Settings().callLanguage()
	case LangDutch, LangEnglish, LangAuto:
	default:
		return VoiceList{}, &Refusal{"Unknown call language."}
	}
	lang = voiceListLanguage(lang)
	if l, ok := s.voices.get(lang, s.now()); ok {
		return l, nil
	}
	el, err := s.elevenLabs()
	if err != nil {
		return VoiceList{}, err
	}
	l, err := buildVoiceList(ctx, el, lang)
	if err != nil {
		return VoiceList{}, err
	}
	s.voices.put(lang, l, s.now())
	return l, nil
}

// AddVoice makes a voice usable by the agent and returns the id to store. A voice already in the account's library
// is returned as is; a Voice Library voice is first added to the library, because ElevenLabs' text to speech and
// agents use voices from the account's library (the docs only document adding shared voices, not using them directly).
func (s *Service) AddVoice(ctx context.Context, voiceID, publicOwnerID string) (string, error) {
	voiceID, publicOwnerID = strings.TrimSpace(voiceID), strings.TrimSpace(publicOwnerID)
	if voiceID == "" || len(voiceID) > maxVoiceIDBytes || len(publicOwnerID) > 128 {
		return "", &Refusal{"That voice id is not valid."}
	}
	el, err := s.elevenLabs()
	if err != nil {
		return "", err
	}
	lib, err := el.libraryVoices(ctx)
	if err != nil {
		return "", err
	}
	for _, v := range lib {
		if v.ID == voiceID {
			return voiceID, nil
		}
	}
	if publicOwnerID == "" {
		return "", &Refusal{"That voice is not in your ElevenLabs library."}
	}
	name := "Orole voice"
	if v, ok := s.voices.find(voiceID); ok && v.Name != "" {
		name = v.Name
	}
	id, err := el.addSharedVoice(ctx, publicOwnerID, voiceID, name)
	if err != nil {
		return "", err
	}
	s.voices.clear()
	return id, nil
}
