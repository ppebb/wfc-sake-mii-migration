package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"go.yaml.in/yaml/v4"
)

type ConnArgs struct {
	Addr         string
	Port         int
	Username     string
	Password     string
	DatabaseName string
}

func main() {
	configBytes, err := os.ReadFile("config.yml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to read config, does config.yml exist?\n%v\n", err)
		os.Exit(1)
	}

	var config ConnArgs
	err = yaml.Unmarshal(configBytes, &config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to parse config: %v\n", err)
		os.Exit(1)
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
	conn, err := pgx.Connect(context.Background(), connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to DB: %v\n", err)
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
		var profileID uint64
		var mii string

		err := rows.Scan(&profileID, mii)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to handle row: %v\n", err)
			os.Exit(1)
		}

		miiBytes, err := base64.StdEncoding.DecodeString(mii)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to decode Mii for profileID %d\n", profileID)
			continue
		}

		// birthMonth and birthDay are on bits 2 through 9
		miiBytes[0x0] = miiBytes[0x0] & 0b0000_0011
		miiBytes[0x1] = miiBytes[0x1] & 0b1111_1100

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

		sanitizedMii := base64.RawStdEncoding.EncodeToString(miiBytes)
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
