// Copyright (c) 2015-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package prompt

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/walletseed"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/hdkeychain/v3"
	"golang.org/x/term"
)

// ProvideSeed is used to prompt for the wallet seed which maybe required during
// upgrades.
func ProvideSeed() ([]byte, error) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Enter existing wallet seed: ")
		seedStr, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		seedStr = strings.TrimSpace(strings.ToLower(seedStr))

		seed, err := hex.DecodeString(seedStr)
		if err != nil || len(seed) < hdkeychain.MinSeedBytes ||
			len(seed) > hdkeychain.MaxSeedBytes {

			fmt.Printf("Invalid seed specified.  Must be a "+
				"hexadecimal value that is at least %d bits and "+
				"at most %d bits\n", hdkeychain.MinSeedBytes*8,
				hdkeychain.MaxSeedBytes*8)
			continue
		}

		return seed, nil
	}
}

// ProvidePrivPassphrase is used to prompt for the private passphrase which
// maybe required during upgrades.
func ProvidePrivPassphrase() ([]byte, error) {
	prompt := "Enter the private passphrase of your wallet: "
	for {
		fmt.Print(prompt)
		pass, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return nil, err
		}
		fmt.Print("\n")
		pass = bytes.TrimSpace(pass)
		if len(pass) == 0 {
			continue
		}

		return pass, nil
	}
}

// promptList prompts the user with the given prefix, list of valid responses,
// and default list entry to use.  The function will repeat the prompt to the
// user until they enter a valid response.
func promptList(reader *bufio.Reader, prefix string, validResponses []string, defaultEntry string) (string, error) {
	// Setup the prompt according to the parameters.
	validStrings := strings.Join(validResponses, "/")
	var prompt string
	if defaultEntry != "" {
		prompt = fmt.Sprintf("%s (%s) [%s]: ", prefix, validStrings,
			defaultEntry)
	} else {
		prompt = fmt.Sprintf("%s (%s): ", prefix, validStrings)
	}

	// Prompt the user until one of the valid responses is given.
	for {
		fmt.Print(prompt)
		reply, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		reply = strings.TrimSpace(strings.ToLower(reply))
		if reply == "" {
			reply = defaultEntry
		}

		for _, validResponse := range validResponses {
			if reply == validResponse {
				return reply, nil
			}
		}
	}
}

// promptListBool prompts the user for a boolean (yes/no) with the given prefix.
// The function will repeat the prompt to the user until they enter a valid
// response.
func promptListBool(reader *bufio.Reader, prefix string, defaultEntry string) (bool, error) {
	// Setup the valid responses.
	valid := []string{"n", "no", "y", "yes"}
	response, err := promptList(reader, prefix, valid, defaultEntry)
	if err != nil {
		return false, err
	}
	return response == "yes" || response == "y", nil
}

// PassPrompt prompts the user for a passphrase with the given prefix.  The
// function will ask the user to confirm the passphrase and will repeat the
// prompts until they enter a matching response.
func PassPrompt(reader *bufio.Reader, prefix string, confirm bool) ([]byte, error) {
	// Prompt the user until they enter a passphrase.
	prompt := fmt.Sprintf("%s: ", prefix)
	for {
		fmt.Print(prompt)
		var pass []byte
		var err error
		fd := int(os.Stdin.Fd())
		if term.IsTerminal(fd) {
			pass, err = term.ReadPassword(fd)
		} else {
			pass, err = reader.ReadBytes('\n')
			if errors.Is(err, io.EOF) {
				err = nil
			}
		}
		if err != nil {
			return nil, err
		}
		fmt.Print("\n")
		pass = bytes.TrimSpace(pass)
		if len(pass) == 0 {
			continue
		}

		if !confirm {
			return pass, nil
		}

		fmt.Print("Confirm passphrase: ")
		var confirm []byte
		if term.IsTerminal(fd) {
			confirm, err = term.ReadPassword(fd)
		} else {
			confirm, err = reader.ReadBytes('\n')
			if errors.Is(err, io.EOF) {
				err = nil
			}
		}
		if err != nil {
			return nil, err
		}
		fmt.Print("\n")
		confirm = bytes.TrimSpace(confirm)
		if !bytes.Equal(pass, confirm) {
			fmt.Println("The entered passphrases do not match")
			continue
		}

		return pass, nil
	}
}

// PrivatePass prompts the user for a private passphrase.  All prompts are
// repeated until the user enters a valid response.
func PrivatePass(reader *bufio.Reader, configPass []byte) ([]byte, error) {
	if len(configPass) > 0 {
		useExisting, err := promptListBool(reader, "Use the "+
			"existing configured private passphrase for "+
			"wallet encryption?", "no")
		if err != nil {
			return nil, err
		}
		if useExisting {
			return configPass, nil
		}
	}
	return PassPrompt(reader, "Enter the private passphrase for your new wallet", true)
}

// PublicPass prompts the user whether they want to add an additional layer of
// encryption to the wallet.  When the user answers yes and there is already a
// public passphrase provided via the passed config, it prompts them whether or
// not to use that configured passphrase.  It will also detect when the same
// passphrase is used for the private and public passphrase and prompt the user
// if they are sure they want to use the same passphrase for both.  Finally, all
// prompts are repeated until the user enters a valid response.
func PublicPass(reader *bufio.Reader, privPass []byte,
	defaultPubPassphrase, configPubPass []byte) ([]byte, error) {

	pubPass := defaultPubPassphrase
	usePubPass, err := promptListBool(reader, "Do you want "+
		"to add an additional layer of encryption for public "+
		"data?", "no")
	if err != nil {
		return nil, err
	}

	if !usePubPass {
		return pubPass, nil
	}

	if len(configPubPass) != 0 && !bytes.Equal(configPubPass, pubPass) {
		useExisting, err := promptListBool(reader, "Use the "+
			"existing configured public passphrase for encryption "+
			"of public data?", "no")
		if err != nil {
			return nil, err
		}

		if useExisting {
			return configPubPass, nil
		}
	}

	for {
		pubPass, err = PassPrompt(reader, "Enter the public "+
			"passphrase for your new wallet", true)
		if err != nil {
			return nil, err
		}

		if bytes.Equal(pubPass, privPass) {
			useSamePass, err := promptListBool(reader,
				"Are you sure want to use the same passphrase "+
					"for public and private data?", "no")
			if err != nil {
				return nil, err
			}

			if useSamePass {
				break
			}

			continue
		}

		break
	}

	fmt.Println("NOTE: Use the --walletpass option to configure your " +
		"public passphrase.")
	return pubPass, nil
}

// Seed prompts the user whether they want to use an existing wallet generation
// seed.  When the user answers no, a seed will be generated and displayed to
// the user along with prompting them for confirmation.  When the user answers
// yes, a the user is prompted for it.  All prompts are repeated until the user
// enters a valid response. The bool returned indicates if the wallet was
// restored from a given seed or not.
func Seed(reader *bufio.Reader) (seed []byte, imported bool, err error) {
	// Ascertain the wallet generation seed.
	useUserSeed, err := promptListBool(reader, "Do you have an "+
		"existing wallet seed you want to use?", "no")
	if err != nil {
		return nil, false, err
	}
	if !useUserSeed {
		seed, err := hdkeychain.GenerateSeed(hdkeychain.RecommendedSeedLen)
		if err != nil {
			return nil, false, err
		}

		seedStrSplit := walletseed.EncodeMnemonicSlice(seed)

		fmt.Println("Your wallet generation seed is:")
		for i := 0; i < hdkeychain.RecommendedSeedLen+1; i++ {
			fmt.Printf("%v ", seedStrSplit[i])

			if (i+1)%6 == 0 {
				fmt.Printf("\n")
			}
		}

		fmt.Printf("\n\nHex: %x\n", seed)
		fmt.Println("IMPORTANT: Keep the seed in a safe place as you\n" +
			"will NOT be able to restore your wallet without it.")
		fmt.Println("Please keep in mind that anyone who has access\n" +
			"to the seed can also restore your wallet thereby\n" +
			"giving them access to all your funds, so it is\n" +
			"imperative that you keep it in a secure location.")

		for {
			fmt.Print(`Once you have stored the seed in a safe ` +
				`and secure location, enter "OK" to continue: `)
			confirmSeed, err := reader.ReadString('\n')
			if err != nil {
				return nil, false, err
			}
			confirmSeed = strings.TrimSpace(confirmSeed)
			confirmSeed = strings.Trim(confirmSeed, `"`)
			if strings.EqualFold("OK", confirmSeed) {
				break
			}
		}

		return seed, false, nil
	}

	for {
		fmt.Print("Enter existing wallet seed " +
			"(follow seed words with additional blank line): ")

		// Use scanner instead of buffio.Reader so we can choose choose
		// more complicated ending condition rather than just a single
		// newline.
		var seedStr string
		scanner := bufio.NewScanner(reader)
		for firstline := true; scanner.Scan(); {
			line := scanner.Text()
			if line == "" {
				break
			}
			if firstline {
				_, err := hex.DecodeString(line)
				if err == nil {
					seedStr = line
					break
				}
				firstline = false
			}
			seedStr += " " + line
		}
		seedStrTrimmed := strings.TrimSpace(seedStr)
		seedStrTrimmed = collapseSpace(seedStrTrimmed)
		wordCount := strings.Count(seedStrTrimmed, " ") + 1

		var seed []byte
		if wordCount == 1 {
			if len(seedStrTrimmed)%2 != 0 {
				seedStrTrimmed = "0" + seedStrTrimmed
			}
			seed, err = hex.DecodeString(seedStrTrimmed)
			if err != nil {
				fmt.Printf("Input error: %v\n", err.Error())
			}
		} else {
			seed, err = walletseed.DecodeUserInput(seedStrTrimmed)
			if err != nil {
				fmt.Printf("Input error: %v\n", err.Error())
			}
		}
		if err != nil || len(seed) < hdkeychain.MinSeedBytes ||
			len(seed) > hdkeychain.MaxSeedBytes {
			fmt.Printf("Invalid seed specified.  Must be a "+
				"word seed (usually 33 words) using the PGP wordlist or "+
				"hexadecimal value that is at least %d bits and "+
				"at most %d bits\n", hdkeychain.MinSeedBytes*8,
				hdkeychain.MaxSeedBytes*8)
			continue
		}

		fmt.Printf("\nSeed input successful. \nHex: %x\n", seed)

		return seed, true, nil
	}
}

// Birthday prompts for a wallet birthday. Return values may be nil with no error.
func Birthday(reader *bufio.Reader) (*time.Time, *uint32, error) {
	for {
		fmt.Printf("Do you have a wallet birthday we should rescan from? (enter date as YYYY-MM-DD, or a block number, or 'no') [no]: ")
		reply, err := reader.ReadString('\n')
		if err != nil {
			return nil, nil, err
		}
		reply = strings.TrimSpace(reply)
		switch strings.ToLower(reply) {
		case "", "n", "no":
			return nil, nil, nil
		case "y", "yes":
			continue
		default:
		}

		// If just a uint assume this is a block number and return that.
		// Ignoring errors and parsing as a birthday below.
		if n, err := strconv.ParseUint(reply, 10, 32); err == nil {
			birthdayBlock := uint32(n)
			fmt.Printf("Using birthday block %d.\n", birthdayBlock)
			return nil, &birthdayBlock, nil
		}

		birthday, err := time.Parse("2006-01-02", reply)
		if err != nil {
			fmt.Printf("Unable to parse date: %v\n", err)
			continue
		}
		if time.Since(birthday) < time.Hour*24 {
			fmt.Println("Birthday cannot be in the future or too close (one day) to the present.")
			continue
		}
		fmt.Printf("Using birthday time %s.\n", birthday)
		return &birthday, nil, nil
	}
}

// ImportedAccounts prompts for any additional account names and xpubs to
// import at wallet creation.
func ImportedAccounts(reader *bufio.Reader, params *chaincfg.Params) (names []string, xpubs []*hdkeychain.ExtendedKey, err error) {
	accounts := make(map[string]struct{})
	accounts["default"] = struct{}{}

	for {
		fmt.Printf("Do you have an additional account to import from an " +
			"extended public key? (enter account name, or 'no') [no]: ")
		reply, err := reader.ReadString('\n')
		if err != nil {
			return nil, nil, err
		}
		reply = strings.TrimSpace(reply)
		switch strings.ToLower(reply) {
		case "", "n", "no":
			return names, xpubs, nil
		case "y", "yes":
			continue
		default:
		}

		account := reply
		if _, ok := accounts[account]; ok {
			fmt.Printf("Account %q is already defined\n", account)
			continue
		}
		fmt.Printf("Enter extended public key for account %q: ", account)
		reply, err = reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return nil, nil, err
		}
		reply = strings.TrimSpace(reply)
		xpub, err := hdkeychain.NewKeyFromString(reply, params)
		if err != nil {
			fmt.Printf("Failed to decode extended key: %v\n", err)
			continue
		}
		if xpub.IsPrivate() {
			fmt.Printf("Extended key is a private key (not neutered)\n")
			continue
		}

		fmt.Printf("Importing account %q from extended public key\n", account)
		names = append(names, account)
		xpubs = append(xpubs, xpub)
		accounts[account] = struct{}{}
	}
}

// collapseSpace takes a string and replaces any repeated areas of whitespace
// with a single space character.
func collapseSpace(in string) string {
	whiteSpace := false
	out := ""
	for _, c := range in {
		if unicode.IsSpace(c) {
			if !whiteSpace {
				out = out + " "
			}
			whiteSpace = true
		} else {
			out = out + string(c)
			whiteSpace = false
		}
	}
	return out
}
