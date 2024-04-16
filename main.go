package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/bogem/id3v2"
	"github.com/cheggaaa/pb/v3"
	_ "github.com/glebarez/go-sqlite"
	"github.com/manifoldco/promptui"
)

type Track struct {
	Id          sql.NullString
	Position    sql.NullInt64
	Title       sql.NullString
	TrackArtist sql.NullString
	Album       sql.NullString
	Year        sql.NullString
	AlbumArtist sql.NullString
}

func main() {

	defer errorHandler()
	//TODO: Переделать обработку ошибок
	userHomeDir, err := os.UserHomeDir()
	checkError(err)
	dbFile, err := getPath("musicdb_*.sqlite")
	checkError(err)
	yMusicDirPath, err := getPath("Music/*")
	checkError(err)

	musicDirPath := filepath.Join(userHomeDir, "Music", "YandexMusicGrabber")

	db, err := sql.Open("sqlite", dbFile)
	checkError(err)
	defer db.Close()

	tracks := getTracks(db)

	if len(tracks) == 0 {
		panic("Не удалось получить список песен")
	}

	// Запросим у пользователя режим работы
	copyMode, err := getRunningMode()
	checkError(err)

	re := regexp.MustCompile(`[\\\/\:\*\?\"\<\>\|]`)

	// Прогресс бар
	bar := pb.ProgressBarTemplate(`{{ red "Копируем:" }} {{ bar . "[" "=" (cycle . "↖" "↗" "↘" "↙" ) "-" "]"}} {{speed . | rndcolor }} {{percent .}}`).Start(len(tracks))
	bar.Set("my_green_string", "green").Set("my_blue_string", "blue")

	for _, track := range tracks {
		ArtistName := "Неизвестный исполнитель"
		if track.AlbumArtist.Valid {
			ArtistName = track.AlbumArtist.String
		} else if track.TrackArtist.Valid {
			ArtistName = track.TrackArtist.String
		}

		srcFile := filepath.Join(yMusicDirPath, track.Id.String+".mp3")
		destFile := ""
		if copyMode == 0 {
			destFile = filepath.Join(musicDirPath, strings.TrimRight(re.ReplaceAllString(ArtistName, "_"), "."), track.Year.String+" - "+strings.TrimRight(re.ReplaceAllString(track.Album.String, "_"), "."), fmt.Sprintf("%02d", track.Position.Int64)+" "+re.ReplaceAllString(track.Title.String, "_")+".mp3")
		} else {
			destFile = filepath.Join(musicDirPath, strings.TrimRight(re.ReplaceAllString(ArtistName, "_"), ".")+" - "+re.ReplaceAllString(track.Title.String, "_")+".mp3")
		}
		err := copyMusicFile(srcFile, destFile)
		checkError(err)

		setTags(destFile, track)

		bar.Increment()
	}
	bar.Finish()

	fmt.Println("Песен скопированно", len(tracks))
	time.Sleep(time.Second * 3)
	exec.Command("explorer", musicDirPath).Start()
}

func copyMusicFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Создаем все папки в пути к файлу
	dir := filepath.Dir(dst)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			return err
		}
	}

	// Проверяем, существует ли файл
	_, err = os.Stat(dst)
	if os.IsNotExist(err) {
		// Если файла не существует, создаем его
		dstFile, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		if _, err := io.Copy(dstFile, srcFile); err != nil {
			return err
		}
	} else if err != nil {
		// Если произошла другая ошибка, возвращаем ее
		return err
	}

	return nil
}

func getPath(pattern string) (string, error) {
	cacheDir, err := os.UserCacheDir()
	checkError(err)

	pattern = filepath.Join(cacheDir, "Packages", "*.Yandex.Music_*", "LocalState", pattern)
	matches, err := filepath.Glob(pattern)
	checkError(err)

	if len(matches) > 0 {
		return matches[0], nil
	}
	err = errors.New("возможно у Вас не установлено приложение Яндекс.Музыка или вы забыли в нём авторизоваться")

	return "", err
}

func getRunningMode() (result int, err error) {

	prompt := promptui.Select{
		Label: "Выберите режим",
		Items: []string{
			"Собрать все песни по папкам (/YandexMusicGrabber/Исполнитель/Альбом/Песни)",
			"Собрать все песни в одну папку (/YandexMusicGrabber/Песни)",
		},
	}

	result, _, err = prompt.Run()

	if err != nil {
		fmt.Printf("Ошибка ввода: %v\n", err)
		return
	}

	return
}

func setTags(destFile string, track Track) {
	tag, err := id3v2.Open(destFile, id3v2.Options{Parse: true})
	checkError(err)
	defer tag.Close()

	tag.SetTitle(track.Title.String)
	tag.SetArtist(track.TrackArtist.String)
	tag.SetAlbum(track.Album.String)
	tag.SetYear(track.Year.String)

	tag.Save() // Не вызываем панику при ошибки сохранения тегов - не критично
}

func errorHandler() {
	if err := recover(); err != nil {
		fmt.Println("Произошла ошибка:", err)
		fmt.Println("Нажмите Enter для завершения...")
		fmt.Scanln()
	}
}

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

func getTracks(db *sql.DB) []Track {
	rows, err := db.Query(`
		SELECT
			T_Track.Id,
			T_TrackAlbum.TrackPosition AS Position,
			T_Track.Title AS Title,
			group_concat(T_Artist.Name, ', ') AS TrackArtist,
			T_Album.Title AS Album,
			T_Album.Year AS Year,
			T_Album.ArtistsString AS AlbumArtist
		FROM
			T_Track
			INNER JOIN T_TrackAlbum ON T_TrackAlbum.TrackId = T_Track.id
			INNER JOIN T_Album ON T_Album.Id = T_TrackAlbum.AlbumId
			INNER JOIN T_TrackArtist ON T_TrackArtist.TrackId = T_Track.id
			INNER JOIN T_Artist ON T_Artist.Id = T_TrackArtist.ArtistId
		WHERE
			IsOffline = 1
		GROUP BY
			T_Track.Id
		ORDER BY
			Album,
			Position
	`)

	checkError(err)

	defer rows.Close()

	var tracks []Track
	for rows.Next() {
		var track Track
		err = rows.Scan(&track.Id, &track.Position, &track.Title, &track.TrackArtist, &track.Album, &track.Year, &track.AlbumArtist)
		checkError(err)
		tracks = append(tracks, track)
	}

	return tracks
}
