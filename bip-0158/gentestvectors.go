// This program connects to your local btcd and generates test vectors for
// 5 blocks and collision space sizes of 1-32 bits. Change the RPC cert path
// and credentials to run on your system. The program assumes you're running
// a btcd with cfilter support, which mainline btcd doesn't have; in order to
// circumvent this assumption, comment out the if block that checks for
// filter size of DefaultP.

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"

	"github.com/roasbeef/btcd/chaincfg/chainhash"
	"github.com/roasbeef/btcd/rpcclient"
	"github.com/roasbeef/btcd/wire"
	"github.com/roasbeef/btcutil/gcs"
	"github.com/roasbeef/btcutil/gcs/builder"
)

var (
	// testBlockHeights are the heights of the blocks to include in the test
	// vectors. Any new entries must be added in sorted order.
	testBlockHeights = []testBlockCase{
		{0, "Genesis block"},
		{1, "Extended filter is empty"},
		{2, ""},
		{3, ""},
		{926485, "Duplicate pushdata 913bcc2be49cb534c20474c4dee1e9c4c317e7eb"},
		{987876, "Coinbase tx has unparseable output script"},
		{1263442, "Includes witness data"},
	}
)

type testBlockCase struct {
	height  uint32
	comment string
}

type JSONTestWriter struct {
	writer          io.Writer
	firstRowWritten bool
}

func NewJSONTestWriter(writer io.Writer) *JSONTestWriter {
	return &JSONTestWriter{writer: writer}
}

func (w *JSONTestWriter) WriteComment(comment string) error {
	return w.WriteTestCase([]interface{}{comment})
}

func (w *JSONTestWriter) WriteTestCase(row []interface{}) error {
	var err error
	if w.firstRowWritten {
		_, err = io.WriteString(w.writer, ",\n")
	} else {
		_, err = io.WriteString(w.writer, "[\n")
		w.firstRowWritten = true
	}
	if err != nil {
		return err
	}

	rowBytes, err := json.Marshal(row)
	if err != nil {
		return err
	}

	_, err = w.writer.Write(rowBytes)
	return err
}

func (w *JSONTestWriter) Close() error {
	if !w.firstRowWritten {
		return nil
	}

	_, err := io.WriteString(w.writer, "\n]\n")
	return err
}

func main() {
	err := os.Mkdir("gcstestvectors", os.ModeDir|0755)
	if err != nil { // Don't overwrite existing output if any
		fmt.Println("Couldn't create directory: ", err)
		return
	}
	files := make([]*JSONTestWriter, 33)
	prevBasicHeaders := make([]chainhash.Hash, 33)
	prevExtHeaders := make([]chainhash.Hash, 33)
	for i := 1; i <= 32; i++ { // Min 1 bit of collision space, max 32
		fName := fmt.Sprintf("gcstestvectors/testnet-%02d.json", i)
		file, err := os.Create(fName)
		if err != nil {
			fmt.Println("Error creating output file: ", err.Error())
			return
		}
		defer file.Close()

		writer := &JSONTestWriter{writer: file}
		defer writer.Close()

		err = writer.WriteComment("Block Height,Block Hash,Block,Previous Basic Header,Previous Ext Header,Basic Filter,Ext Filter,Basic Header,Ext Header,Notes")
		if err != nil {
			fmt.Println("Error writing to output file: ", err.Error())
			return
		}

		files[i] = writer
	}
	cert, err := ioutil.ReadFile(
		path.Join(os.Getenv("HOME"), "/.btcd/rpc.cert"))
	if err != nil {
		fmt.Println("Couldn't read RPC cert: ", err.Error())
		return
	}
	conf := rpcclient.ConnConfig{
		Host:         "127.0.0.1:18334",
		Endpoint:     "ws",
		User:         "kek",
		Pass:         "kek",
		Certificates: cert,
	}
	client, err := rpcclient.New(&conf, nil)
	if err != nil {
		fmt.Println("Couldn't create a new client: ", err.Error())
		return
	}

	var testBlockIndex int = 0
	for height := 0; testBlockIndex < len(testBlockHeights); height++ {
		fmt.Printf("Height: %d\n", height)
		blockHash, err := client.GetBlockHash(int64(height))
		if err != nil {
			fmt.Println("Couldn't get block hash: ", err.Error())
			return
		}
		block, err := client.GetBlock(blockHash)
		if err != nil {
			fmt.Println("Couldn't get block hash: ", err.Error())
			return
		}
		var blockBuf bytes.Buffer
		err = block.Serialize(&blockBuf)
		if err != nil {
			fmt.Println("Error serializing block to buffer: ", err.Error())
			return
		}
		blockBytes := blockBuf.Bytes()
		for i := 1; i <= 32; i++ {
			basicFilter, err := buildBasicFilter(block, uint8(i))
			if err != nil {
				fmt.Println("Error generating basic filter: ", err.Error())
				return
			}
			basicHeader, err := builder.MakeHeaderForFilter(basicFilter,
				prevBasicHeaders[i])
			if err != nil {
				fmt.Println("Error generating header for filter: ", err.Error())
				return
			}
			if basicFilter == nil {
				basicFilter = &gcs.Filter{}
			}
			extFilter, err := buildExtFilter(block, uint8(i))
			if err != nil {
				fmt.Println("Error generating ext filter: ", err.Error())
				return
			}
			extHeader, err := builder.MakeHeaderForFilter(extFilter,
				prevExtHeaders[i])
			if err != nil {
				fmt.Println("Error generating header for filter: ", err.Error())
				return
			}
			if extFilter == nil {
				extFilter = &gcs.Filter{}
			}
			if i == builder.DefaultP { // This is the default filter size so we can check against the server's info
				filter, err := client.GetCFilter(blockHash, wire.GCSFilterRegular)
				if err != nil {
					fmt.Println("Error getting basic filter: ", err.Error())
					return
				}
				nBytes, err := basicFilter.NBytes()
				if err != nil {
					fmt.Println("Couldn't get NBytes(): ", err)
					return
				}
				if !bytes.Equal(filter.Data, nBytes) {
					// Don't error on empty filters
					fmt.Println("Basic filter doesn't match!\n", filter.Data, "\n", nBytes)
					return
				}
				filter, err = client.GetCFilter(blockHash, wire.GCSFilterExtended)
				if err != nil {
					fmt.Println("Error getting extended filter: ", err.Error())
					return
				}
				nBytes, err = extFilter.NBytes()
				if err != nil {
					fmt.Println("Couldn't get NBytes(): ", err)
					return
				}
				if !bytes.Equal(filter.Data, nBytes) {
					fmt.Println("Extended filter doesn't match!")
					return
				}
				header, err := client.GetCFilterHeader(blockHash, wire.GCSFilterRegular)
				if err != nil {
					fmt.Println("Error getting basic header: ", err.Error())
					return
				}
				if !bytes.Equal(header.PrevFilterHeader[:], basicHeader[:]) {
					fmt.Println("Basic header doesn't match!")
					return
				}
				header, err = client.GetCFilterHeader(blockHash, wire.GCSFilterExtended)
				if err != nil {
					fmt.Println("Error getting extended header: ", err.Error())
					return
				}
				if !bytes.Equal(header.PrevFilterHeader[:], extHeader[:]) {
					fmt.Println("Extended header doesn't match!")
					return
				}
				fmt.Println("Verified against server")
			}

			if uint32(height) == testBlockHeights[testBlockIndex].height {
				var bfBytes []byte
				var efBytes []byte
				bfBytes, err = basicFilter.NBytes()
				if err != nil {
					fmt.Println("Couldn't get NBytes(): ", err)
					return
				}
				efBytes, err = extFilter.NBytes()
				if err != nil {
					fmt.Println("Couldn't get NBytes(): ", err)
					return
				}
				row := []interface{}{
					height,
					blockHash.String(),
					hex.EncodeToString(blockBytes),
					prevBasicHeaders[i].String(),
					prevExtHeaders[i].String(),
					hex.EncodeToString(bfBytes),
					hex.EncodeToString(efBytes),
					basicHeader.String(),
					extHeader.String(),
					testBlockHeights[testBlockIndex].comment,
				}
				err = files[i].WriteTestCase(row)
				if err != nil {
					fmt.Println("Error writing test case to output: ", err.Error())
					return
				}
			}
			prevBasicHeaders[i] = basicHeader
			prevExtHeaders[i] = extHeader
		}

		if uint32(height) == testBlockHeights[testBlockIndex].height {
			testBlockIndex++
		}
	}
}

// buildBasicFilter builds a basic GCS filter from a block. A basic GCS filter
// will contain all the previous outpoints spent within a block, as well as the
// data pushes within all the outputs created within a block. p is specified as
// an argument in order to create test vectors with various values for p.
func buildBasicFilter(block *wire.MsgBlock, p uint8) (*gcs.Filter, error) {
	blockHash := block.BlockHash()
	b := builder.WithKeyHashP(&blockHash, p)

	// If the filter had an issue with the specified key, then we force it
	// to bubble up here by calling the Key() function.
	_, err := b.Key()
	if err != nil {
		return nil, err
	}

	// In order to build a basic filter, we'll range over the entire block,
	// adding the outpoint data as well as the data pushes within the
	// pkScript.
	for i, tx := range block.Transactions {
		// First we'll compute the bash of the transaction and add that
		// directly to the filter.
		txHash := tx.TxHash()
		b.AddHash(&txHash)

		// Skip the inputs for the coinbase transaction
		if i != 0 {
			// Each each txin, we'll add a serialized version of
			// the txid:index to the filters data slices.
			for _, txIn := range tx.TxIn {
				b.AddOutPoint(txIn.PreviousOutPoint)
			}
		}

		// For each output in a transaction, we'll add each of the
		// individual data pushes within the script.
		for _, txOut := range tx.TxOut {
			b.AddEntry(txOut.PkScript)
		}
	}

	return b.Build()
}

// buildExtFilter builds an extended GCS filter from a block. An extended
// filter supplements a regular basic filter by include all the _witness_ data
// found within a block. This includes all the data pushes within any signature
// scripts as well as each element of an input's witness stack. Additionally,
// the _hashes_ of each transaction are also inserted into the filter. p is
// specified as an argument in order to create test vectors with various values
// for p.
func buildExtFilter(block *wire.MsgBlock, p uint8) (*gcs.Filter, error) {
	blockHash := block.BlockHash()
	b := builder.WithKeyHashP(&blockHash, p)

	// If the filter had an issue with the specified key, then we force it
	// to bubble up here by calling the Key() function.
	_, err := b.Key()
	if err != nil {
		return nil, err
	}

	// In order to build an extended filter, we add the hash of each
	// transaction as well as each piece of witness data included in both
	// the sigScript and the witness stack of an input.
	for i, tx := range block.Transactions {
		// Skip the inputs for the coinbase transaction
		if i != 0 {
			// Next, for each input, we'll add the sigScript (if
			// it's present), and also the witness stack (if it's
			// present)
			for _, txIn := range tx.TxIn {
				if txIn.SignatureScript != nil {
					b.AddScript(txIn.SignatureScript)
				}

				if len(txIn.Witness) != 0 {
					b.AddWitness(txIn.Witness)
				}
			}
		}
	}

	return b.Build()
}
