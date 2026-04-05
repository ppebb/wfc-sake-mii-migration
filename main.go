package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"go.yaml.in/yaml/v4"
	"golang.org/x/text/encoding/unicode"
)

type ConnArgs struct {
	Addr         string
	Port         int
	Username     string
	Password     string
	DatabaseName string
}

const NameLen = 10

type MiiData struct {
	birthDay    uint8
	birthMonth  uint8
	miiID       uint32
	sysID       uint32
	miiName     string
	creatorName string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "No args provided! Pass one of 'sanitize', 'verify', 'file', or 'print'.\n")
		os.Exit(1)
	}

	subCommand := os.Args[1]

	switch subCommand {
	case "sanitize":
		sanitizeDB()
	case "verify":
		verifyDB()
	case "file":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "File subcommand missing path argument!\n")
			os.Exit(1)
		}
		sanitizeFile(os.Args[2])
	case "print":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Print subcommand missing path argument!\n")
			os.Exit(1)
		}

		bytes, err := os.ReadFile(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read file '%s': %s\n", os.Args[2], err)
			os.Exit(1)
		}

		miiData, err := decodeMii(bytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to decode Mii file '%s': %v\n", os.Args[2], err)
		}

		printMii(miiData)
	default:
		fmt.Fprintf(os.Stderr, "Unknown sub command: %s\n", subCommand)
		os.Exit(1)
	}
}

func printMii(miiData MiiData) {
	fmt.Println("Stored Mii Fields:")
	fmt.Printf("Mii: %s\n", miiData.miiName)
	fmt.Printf("Birth Date (mm/dd): %d/%d\n", miiData.birthMonth, miiData.birthDay)
	fmt.Printf("MiiID: %b\n", miiData.miiID)
	fmt.Printf("SysID: %b\n", miiData.sysID)
	fmt.Printf("Creator Name: %s\n", miiData.creatorName)
}

var decoder = unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewDecoder()

func decodeName(bytes []byte) (string, error) {
	i := 0
	for ; i < 10; i++ {
		if binary.BigEndian.Uint16(bytes[i*2:i*2+2]) == 0 {
			break
		}
	}

	strBytes, err := decoder.Bytes(bytes[0 : i*2])
	if err != nil {
		return "", err
	}

	return string(strBytes), nil
}

func decodeMii(bytes []byte) (MiiData, error) {
	b0 := bytes[0x0]
	b1 := bytes[0x1]
	birthMonth := b0 >> 2 & 0b1111
	birthDay := b0&0b11<<3 | b1>>5
	miiID := binary.BigEndian.Uint32(bytes[0x18:0x1C])
	sysID := binary.BigEndian.Uint32(bytes[0x1C:0x20])

	miiName, err := decodeName(bytes[0x2:0x15])
	if err != nil {
		return MiiData{}, err
	}

	creatorName, err := decodeName(bytes[0x36:0x49])
	if err != nil {
		return MiiData{}, err
	}

	return MiiData{
		birthDay,
		birthMonth,
		miiID,
		sysID,
		miiName,
		creatorName,
	}, nil
}

func sanitizeFile(file string) {
	bytes, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read file '%s': %v\n", file, err)
		os.Exit(1)
	}

	miiData, err := decodeMii(bytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to decode Mii '%s': %v\n", file, err)
	}

	printMii(miiData)

	fmt.Println("Sanitizing Mii in-place...")

	bytes = bytesSanitize(bytes)
	os.WriteFile(file, bytes, 0644)
}

func connectDB() (*pgx.Conn, error) {
	configBytes, err := os.ReadFile("config.yml")
	if err != nil {
		return nil, err
	}

	var config ConnArgs
	err = yaml.Unmarshal(configBytes, &config)
	if err != nil {
		return nil, err
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Ensure you have made a backup using pg_dump prior to running this!")
	fmt.Println("Press enter to continue...")
	reader.ReadString('\n')

	fmt.Println("Connecting to DB...")

	connStr := fmt.Sprintf("postgresql://%s:%s@%s:%d/%s",
		config.Username,
		config.Password,
		config.Addr,
		config.Port,
		config.DatabaseName,
	)

	return pgx.Connect(context.Background(), connStr)
}

func sanitizeDB() {
	conn, err := connectDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to DB: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Connected.")
	defer conn.Close(context.Background())

	rows, err := conn.Query(context.Background(), "SELECT profile_id, mariokartwii_friend_info FROM users")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to query mkw_friend_info: %v\n", err)
		os.Exit(1)
	}

	counter := 0
	fmt.Println("Processing Miis...")

	for rows.Next() {
		var profileID uint32
		var mii string

		err := rows.Scan(&profileID, mii)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to handle row: %v\n", err)
			os.Exit(1)
		}

		sanitizedMii, err := b64Sanitize(profileID, mii)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to sanitize Mii: %v\n", err)
		}

		_, err = conn.Exec(context.Background(), "UPDATE users SET mariokartwii_friend_info = $2 WHERE profile_id = $1", profileID, sanitizedMii)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write sanitized mii for profileID %d\n", profileID)
		}

		if counter%1000 == 0 {
			fmt.Printf("Processed %d Miis...\n", counter)
		}
	}

	fmt.Printf("Finished processing Miis! Replaces %d records with sanitized Miis.\n", counter)
}

func verifyDB() {
	conn, err := connectDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to DB: %v\n", err)
		os.Exit(1)
	}

	rows, err := conn.Query(context.Background(), "SELECT profile_id, mariokartwii_friend_info FROM users")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to query mkw_friend_info: %v\n", err)
		os.Exit(1)
	}

	counter := 0
	fmt.Println("Verifying Miis...")

	for rows.Next() {
		counter++

		var profileID uint32
		var mii string

		err := rows.Scan(&profileID, mii)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to handle row: %v\n", err)
			os.Exit(1)
		}

		miiBytes, err := base64.StdEncoding.DecodeString(mii)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to decode b64 Mii for profileID %d: %v\n", profileID, err)
			continue
		}

		miiData, err := decodeMii(miiBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to decode Mii for profileID %d: %v\n", profileID, err)
		}

		if miiData.birthDay != 0 || miiData.birthMonth != 0 || (miiData.miiID>>3) != 0 || miiData.creatorName != "" {
			fmt.Fprintf(os.Stderr, "ProfileID %d has an unsanitized Mii!\n", profileID)
			printMii(miiData)
		}

		if counter%1000 == 0 {
			fmt.Printf("Verified %d Miis...\n", counter)
		}
	}
}

// Sanitize a b64 encoded Mii
func b64Sanitize(profileID uint32, mii string) (string, error) {
	miiBytes, err := base64.StdEncoding.DecodeString(mii)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to decode Mii for profileID %d\n", profileID)
		return "", err
	}

	miiBytes = bytesSanitize(miiBytes)

	return base64.RawStdEncoding.EncodeToString(miiBytes), nil
}

func bytesSanitize(miiBytes []byte) []byte {
	// birthMonth and birthDay are on bits 2 through 9
	miiBytes[0x0] = miiBytes[0x0] & 0b11000000
	miiBytes[0x1] = miiBytes[0x1] & 0b00011111

	// We do not want to zero out the sysID, but others may. These are the
	// relevant bytes.
	// for i := range 4 {
	// 	// Zero sysid
	// 	miiBytes[0x1C+i] = 0
	// }

	// Zero creation timestamp
	// Keep top 3 bits of the first byte (special, foreign, regular)
	miiBytes[0x18] = miiBytes[0x18] & 0xE0
	miiBytes[0x19] = 0
	miiBytes[0x1A] = 0
	miiBytes[0x1B] = 0

	// Zero creator name
	for i := 0x36; i < 0x49; i++ {
		miiBytes[i] = 0
	}

	return miiBytes
}
