package report

import (
	"bytes"
	"context"
	"database/sql"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAtlasLowOpacityEdgesChangeBackground(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, atlasTileSize, atlasTileSize))
	background := color.RGBA{9, 15, 25, 255}
	draw.Draw(img, img.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
	atlasLine(img, 10, 10, 100, 10, color.RGBA{95, 114, 140, 2})
	require.NotEqual(t, background, pixelRGB(img, 50, 10), "low-opacity topology must survive pixel quantization")
}

func TestSparseAtlasOverviewHasVisibleEdgesAndHosts(t *testing.T) {
	_, path, info := atlasFixture(t, 2)
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`UPDATE nodes SET x=CASE id WHEN 1 THEN .1 ELSE .9 END,y=.4,rank=CASE id WHEN 1 THEN .9 ELSE .1 END;DELETE FROM spatial;INSERT INTO spatial SELECT id,x,x,y,y FROM nodes;DROP TABLE edge_spatial;DROP TABLE tiles;DROP INDEX node_authority;`)
	require.NoError(t, err)
	require.NoError(t, buildAtlasRaster(context.Background(), db, info, func(string) {}))
	ForgetAtlas(path)
	tile, err := AtlasTile(context.Background(), path, AtlasTileRequest{Snapshot: info.ID})
	require.NoError(t, err)
	img, err := png.Decode(bytes.NewReader(tile.PNG))
	require.NoError(t, err)
	require.Greater(t, pixelRGB(img, 256, 205).B, uint8(55), "a sparse overview needs a visible connecting edge")
	colored := 0
	for y := 201; y < 209; y++ {
		for x := 47; x < 56; x++ {
			if pixelRGB(img, x, y).R > 150 {
				colored++
			}
		}
	}
	require.GreaterOrEqual(t, colored, 8, "hosts must remain distinguishable from isolated pixels")
	lessImportant := 0
	for y := 201; y < 209; y++ {
		for x := 456; x < 466; x++ {
			if pixelRGB(img, x, y).R > 150 {
				lessImportant++
			}
		}
	}
	require.Greater(t, colored, lessImportant, "visible node size must still reflect authority")
}
