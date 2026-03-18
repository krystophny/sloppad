package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/krystophny/tabura/internal/googleauth"
)

func cmdGoogleAuth() error {
	session, err := googleauth.New("", "", googleauth.DefaultScopes)
	if err != nil {
		return err
	}
	fmt.Println("Open this URL in your browser:")
	fmt.Println()
	fmt.Println(session.GetAuthURL())
	fmt.Println()
	fmt.Print("Paste the authorization code: ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return fmt.Errorf("no input")
	}
	code := strings.TrimSpace(scanner.Text())
	if code == "" {
		return fmt.Errorf("empty authorization code")
	}
	if err := session.ExchangeCode(context.Background(), code); err != nil {
		return err
	}
	fmt.Printf("Token saved to %s\n", session.TokenPath())
	return nil
}
