package bip

import (
	"encoding/hex"
	"fmt"
	"github.com/Aurora/aurora-go-sdk/aoa"
	"github.com/tyler-smith/go-bip32"
	"github.com/tyler-smith/go-bip39"
	"testing"
)

func TestNewKeyFromMnemonic1(t *testing.T) {

	// Generate a mnemonic for memorization or user-friendly seeds
	entropy, _ := bip39.NewEntropy(128)
	mnemonic, _ := bip39.NewMnemonic(entropy)

	// Generate a Bip32 HD wallet for the mnemonic and a user supplied password
	seed := bip39.NewSeed(mnemonic, "Secret Passphrase")

	masterKey, _ := bip32.NewMasterKey(seed)
	publicKey := masterKey.PublicKey()

	// Display mnemonic and keys
	fmt.Println("Mnemonic: ", mnemonic)
	fmt.Println("Master private key: ", len(hex.EncodeToString(masterKey.Key)))
	fmt.Println("Master public key: ", publicKey)

}

func TestNewKeyFromMasterKey(t *testing.T) {

	entropy, _ := bip39.NewEntropy(128)

	mnemonic, _ := bip39.NewMnemonic(entropy)

	seed, err := bip39.NewSeedWithErrorChecking(mnemonic, "")
	if err != nil {
		panic(err)
	}

	masterKey, err := bip32.NewMasterKey(seed)
	if err != nil {
		panic(err)
	}

	fKey, err := NewKeyFromMasterKey(masterKey, TypeEther, bip32.FirstHardenedChild, 0, 0)
	fmt.Println("Mnemonic: ", mnemonic)
	fmt.Println("Master private key: ", masterKey)
	fmt.Println(hex.EncodeToString(fKey.Key))
	fmt.Println(aoa.GetNewAddress(hex.EncodeToString(fKey.Key)).String())
}
