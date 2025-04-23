package signer

import (
	"errors"
	"fmt"

	cmtcrypto "github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/p2p"
	cmtp2p "github.com/cometbft/cometbft/p2p"
	"github.com/libp2p/go-libp2p/core/crypto"

	"github.com/rollkit/rollkit/pkg/signer"
)

var (
	errNilKey             = errors.New("node key is nil")
	errUnsupportedKeyType = errors.New("unsupported key type")
)

var _ signer.Signer = (*signerWrapper)(nil)

type signerWrapper struct {
	cmtPrivKey cmtcrypto.PrivKey
	p2pPrivKey crypto.PrivKey
}

func NewSignerWrapper(cmtPrivKey cmtcrypto.PrivKey) signer.Signer {
	p2pPrivKey, err := GetNodeKey(&cmtp2p.NodeKey{PrivKey: cmtPrivKey})
	if err != nil {
		panic("failed to get node key")
	}

	return &signerWrapper{
		cmtPrivKey: cmtPrivKey,
		p2pPrivKey: p2pPrivKey,
	}
}

// GetAddress implements signer.Signer.
func (s *signerWrapper) GetAddress() ([]byte, error) {
	return s.cmtPrivKey.PubKey().Address().Bytes(), nil
}

// GetPublic implements signer.Signer.
func (s *signerWrapper) GetPublic() (crypto.PubKey, error) {
	return s.p2pPrivKey.GetPublic(), nil
}

// Sign implements signer.Signer.
func (s *signerWrapper) Sign(message []byte) ([]byte, error) {
	return s.p2pPrivKey.Sign(message)
}

// GetNodeKey creates libp2p private key from Tendermints NodeKey.
func GetNodeKey(nodeKey *p2p.NodeKey) (crypto.PrivKey, error) {
	if nodeKey == nil || nodeKey.PrivKey == nil {
		return nil, errNilKey
	}

	switch nodeKey.PrivKey.Type() {
	case "ed25519":
		privKey, err := crypto.UnmarshalEd25519PrivateKey(nodeKey.PrivKey.Bytes())
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling node private key: %w", err)
		}
		return privKey, nil
	default:
		return nil, errUnsupportedKeyType
	}
}
