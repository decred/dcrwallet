// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2016 The Decred developers
// Copyright (c) 2017-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package legacyrpc

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"io/ioutil"

	"github.com/decred/dcrd/hdkeychain"
	"github.com/decred/dcrd/chainhash"
)

// Contracthash gets the contract hash from the inputted hashedContracts
func ContractHash(hashedContracts [][]byte) (hdkeychain.ExtendedKey, error) {
	// append the hashed contracts i.e h1 + h2 + h3 to create combinedContractHashes
	combinedContractHashes := make([]byte, 1)
	for i := range hashedContracts {
		for j := range hashedContracts {
			combinedContractHashes = append(combinedContractHashes, hashedContracts[i][j])
		}
	}

	// apply the blake256 hash function to the combinedContractHashes
	contractsHashed := chainhash.HashB(combinedContractHashes)

	// build the contract hash extended key 
	hashChan := make(chan hdkeychain.ExtendedKey)
	go func (contractsHashed []byte) {
		segmentedHashedContracts := make([][]byte, 16)
		uint32segmentedHashedContracts := make([]uint32, 16)
		for i := 0; i < 16; i++ {
			switch i {
			case i == 0:
				segmentedHashedContracts[i] = contractsHashed[i*5 : (i*5)+5]
				uint32segmentedHashedContracts[i] = byteToUInt32(segmentedHashedContracts[i])
				contractHash := hdkeychain.NewKeyFromString(string(segmentedHashedContracts[i])
			case i >= 1:
				segmentedHashedContracts[i] = contractsHashed[i*5 : (i*5)+5]
				uint32segmentedHashedContracts[i] = byteToUInt32(segmentedHashedContracts[i])
				contractHash, err := contractHash.Child(uint32segmentedHashedContracts[i])
				if err != nil {
					return nil, err
				}
			}
		}

		hashChan <- contractHash
	}(contractsHashed)


	return <- hashChan, nil
}

// createContractArray creates a array of contracts from the input filepath slice.
//
// TODO: convert text to a input standard here before ReadFile path.
// or add error checking for specific text format
func createContractArray(filePaths []string) ([][]byte, error) {
	var contractArray = make([][]byte, len(filePaths))
	for i := range filePaths {
		contractArray[i], _ = ioutil.ReadFile(filePaths[i])
	}

	if len(contractArray) != len(filePaths) {
		return nil, fmt.Errorf("Contract Array was not created successfully")
	}

	return contractArray, nil
}

func byteToUInt32(data []byte) (ret uint32) {
	buf := bytes.NewBuffer(data)
	binary.Read(buf, binary.LittleEndian, &ret)
	return
}

// hashContracts hashes contracts and places them in a lexigrahpically assorted array.
func hashContracts(contractArray [][]byte) [][]byte {
	hashedContracts := make([][]byte, len(contractArray))

	for i := range contractArray {
		hashedContracts[i] = chainhash.HashB(contractArray[i])
	}

	return lexiSort(hashedContracts)
}

func lexiSort(hashedContracts [][]byte) ([][]byte, error) {
	for i := range hashedContracts {
		for j := range hashedContracts {
			if len(hashedContracts[i][j]) != 256 {
				return nil, fmt.Errorf("Contract should be a size of 256 bits")
			} else if hashedContracts[i][j] > hashedContracts[i][j+1] {
				hashedContracts[i], hashedContracts[i+1] = hashedContracts[i+1], hashedContracts[i]
			}
		}
	}

	return hashedContracts, nil
}
