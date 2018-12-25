package build

import (
	"fmt"
	"os"

	storage "github.com/Azure/azure-storage-go"
)

type AzureBlobstoreConfig struct {
	Account   string 
	Token     string 
	Container string 
}

func AzureBlobstoreUpload(path string, name string, config AzureBlobstoreConfig) error {
	if *DryRunFlag {
		fmt.Printf("would upload %q to %s/%s/%s\n", path, config.Account, config.Container, name)
		return nil
	}

	rawClient, err := storage.NewBasicClient(config.Account, config.Token)
	if err != nil {
		return err
	}
	client := rawClient.GetBlobService()

	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	return client.CreateBlockBlobFromReader(config.Container, name, uint64(info.Size()), in, nil)
}

func AzureBlobstoreList(config AzureBlobstoreConfig) ([]storage.Blob, error) {

	rawClient, err := storage.NewBasicClient(config.Account, config.Token)
	if err != nil {
		return nil, err
	}
	client := rawClient.GetBlobService()

	container := client.GetContainerReference(config.Container)

	blobs, err := container.ListBlobs(storage.ListBlobsParameters{
		MaxResults: 1024 * 1024 * 1024, 
		Timeout:    3600,               
	})
	if err != nil {
		return nil, err
	}
	return blobs.Blobs, nil
}

func AzureBlobstoreDelete(config AzureBlobstoreConfig, blobs []storage.Blob) error {
	if *DryRunFlag {
		for _, blob := range blobs {
			fmt.Printf("would delete %s (%s) from %s/%s\n", blob.Name, blob.Properties.LastModified, config.Account, config.Container)
		}
		return nil
	}

	rawClient, err := storage.NewBasicClient(config.Account, config.Token)
	if err != nil {
		return err
	}
	client := rawClient.GetBlobService()

	for _, blob := range blobs {
		if err := client.DeleteBlob(config.Container, blob.Name, nil); err != nil {
			return err
		}
	}
	return nil
}
