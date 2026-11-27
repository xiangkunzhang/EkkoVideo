// Package local provides local filesystem browsing, natural episode sorting,
// sibling playlist generation, and external subtitle auto-discovery.
package local

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/xiangkunzhang/Velan/internal/core"
)

const SourceID = "local"

var videoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".mov": true, ".avi": true,
	".flv": true, ".webm": true, ".ts": true, ".m2ts": true,
	".m4v": true, ".wmv": true, ".rmvb": true, ".mpg": true, ".mpeg": true,
}

var subExts = map[string]bool{
	".srt": true, ".ass": true, ".ssa": true, ".vtt": true,
}

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Info() core.ProviderInfo {
	return core.ProviderInfo{
		ID:        SourceID,
		Name:      "本地视频",
		Type:      core.ProviderLocal,
		ListStyle: core.ListFileBrowser,
	}
}

func IsVideoFile(name string) bool {
	return videoExts[strings.ToLower(filepath.Ext(name))]
}

func (p *Provider) List(_ context.Context, folder core.ItemRef, _ string) (core.Page, error) {
	dir := folder.ID
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			dir = "/"
		} else {
			movies := filepath.Join(home, "Movies")
			if st, err := os.Stat(movies); err == nil && st.IsDir() {
				dir = movies
			} else {
				dir = home
			}
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return core.Page{}, err
	}

	var dirs, files []core.Item
	for _, ent := range entries {
		name := ent.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		if ent.IsDir() {
			dirs = append(dirs, core.Item{
				Ref:      core.ItemRef{Source: SourceID, Kind: core.KindFolder, ID: full},
				Title:    name,
				Subtitle: "文件夹",
				Playable: false,
			})
			continue
		}
		if IsVideoFile(name) {
			var sub string
			if info, err := ent.Info(); err == nil {
				sub = formatSize(info.Size())
			}
			files = append(files, core.Item{
				Ref:      core.ItemRef{Source: SourceID, Kind: core.KindFile, ID: full},
				Title:    name,
				Subtitle: sub,
				Playable: true,
			})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return NaturalLess(dirs[i].Title, dirs[j].Title) })
	sort.Slice(files, func(i, j int) bool { return NaturalLess(files[i].Title, files[j].Title) })

	items := make([]core.Item, 0, len(dirs)+len(files))
	items = append(items, dirs...)
	items = append(items, files...)
	return core.Page{Items: items, Total: len(items)}, nil
}

func (p *Provider) Resolve(_ context.Context, ref core.ItemRef, _ core.ResolveOpts) (*core.PlaySource, error) {
	path := ref.ID
	return &core.PlaySource{
		Ref:       ref,
		Title:     filepath.Base(path),
		URL:       path,
		Kind:      core.StreamLocalFile,
		Subtitles: DiscoverSubtitles(path),
	}, nil
}

// BuildQueue builds a naturally sorted sibling video queue for a given file path.
func (p *Provider) BuildQueue(filePath string) *core.Queue {
	dir := filepath.Dir(filePath)
	page, err := p.List(context.Background(), core.ItemRef{Source: SourceID, Kind: core.KindFolder, ID: dir}, "")
	if err != nil {
		return core.NewQueue([]core.Item{{
			Ref:      core.ItemRef{Source: SourceID, Kind: core.KindFile, ID: filePath},
			Title:    filepath.Base(filePath),
			Playable: true,
		}}, 0)
	}
	var videos []core.Item
	idx := -1
	for _, it := range page.Items {
		if it.Ref.Kind == core.KindFile && it.Playable {
			if it.Ref.ID == filePath {
				idx = len(videos)
			}
			videos = append(videos, it)
		}
	}
	if idx < 0 {
		videos = append([]core.Item{{
			Ref:      core.ItemRef{Source: SourceID, Kind: core.KindFile, ID: filePath},
			Title:    filepath.Base(filePath),
			Playable: true,
		}}, videos...)
		idx = 0
	}
	return core.NewQueue(videos, idx)
}

// DiscoverSubtitles finds matching external subtitles beside videoPath or inside Subs/.
func DiscoverSubtitles(videoPath string) []core.SubtitleTrack {
	dir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	var tracks []core.SubtitleTrack
	seen := map[string]bool{}

	scan := func(d string, requirePrefix bool) {
		ents, err := os.ReadDir(d)
		if err != nil {
			return
		}
		for _, ent := range ents {
			if ent.IsDir() {
				continue
			}
			name := ent.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if !subExts[ext] {
				continue
			}
			if requirePrefix && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(base)) {
				continue
			}
			full := filepath.Join(d, name)
			if seen[full] {
				continue
			}
			seen[full] = true
			tracks = append(tracks, core.SubtitleTrack{
				Label: name,
				URL:   full,
			})
		}
	}

	scan(dir, true)
	scan(filepath.Join(dir, "Subs"), false)
	scan(filepath.Join(dir, "Subtitles"), false)
	return tracks
}

// NaturalLess compares strings with embedded numbers numerically (e.g. "ep2" < "ep10").
func NaturalLess(a, b string) bool {
	ia, ib := 0, 0
	for ia < len(a) && ib < len(b) {
		ca, cb := rune(a[ia]), rune(b[ib])
		if unicode.IsDigit(ca) && unicode.IsDigit(cb) {
			na, ea := readNum(a, ia)
			nb, eb := readNum(b, ib)
			if na != nb {
				return na < nb
			}
			ia, ib = ea, eb
			continue
		}
		la, lb := unicode.ToLower(ca), unicode.ToLower(cb)
		if la != lb {
			return la < lb
		}
		ia++
		ib++
	}
	return len(a) < len(b)
}

func readNum(s string, start int) (int64, int) {
	end := start
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, _ := strconv.ParseInt(s[start:end], 10, 64)
	return n, end
}

func formatSize(b int64) string {
	const mb = 1024 * 1024
	const gb = 1024 * mb
	switch {
	case b >= gb:
		return strconv.FormatFloat(float64(b)/float64(gb), 'f', 1, 64) + " GB"
	case b >= mb:
		return strconv.FormatFloat(float64(b)/float64(mb), 'f', 1, 64) + " MB"
	default:
		return strconv.FormatInt(b/1024, 10) + " KB"
	}
}
