package channeldb

import (
	"encoding/hex"
	"testing"

	"github.com/lightningnetwork/lnd/channeldb/models"
	"github.com/lightningnetwork/lnd/kvdb"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/stretchr/testify/require"
)

var (
	pubKey1Bytes, _ = hex.DecodeString(
		"0248f5cba4c6da2e4c9e01e81d1404dfac0cbaf3ee934a4fc117d2ea9a64" +
			"22c91d",
	)
	pubKey2Bytes, _ = hex.DecodeString(
		"038155ba86a8d3b23c806c855097ca5c9fa0f87621f1e7a7d2835ad057f6" +
			"f4484f",
	)

	pubKey1, _ = route.NewVertexFromBytes(pubKey1Bytes)
	pubKey2, _ = route.NewVertexFromBytes(pubKey2Bytes)
)

type node struct {
	pubKey   route.Vertex
	features *lnwire.FeatureVector

	edgeInfos   []*models.ChannelEdgeInfo1
	outPolicies []*models.ChannelEdgePolicy1
	inPolicies  []*models.ChannelEdgePolicy1
}

func (n *node) PubKey() route.Vertex {
	return n.pubKey
}
func (n *node) Features() *lnwire.FeatureVector {
	return n.features
}

func (n *node) ForEachChannel(tx kvdb.RTx,
	cb func(kvdb.RTx, *models.ChannelEdgeInfo1, *models.ChannelEdgePolicy1,
		*models.ChannelEdgePolicy1) error) error {

	for idx := range n.edgeInfos {
		err := cb(
			tx, n.edgeInfos[idx], n.outPolicies[idx],
			n.inPolicies[idx],
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// TestGraphCacheAddNode tests that a channel going from node A to node B can be
// cached correctly, independent of the direction we add the channel as.
func TestGraphCacheAddNode(t *testing.T) {
	t.Parallel()

	runTest := func(nodeA, nodeB route.Vertex) {
		t.Helper()

		channelFlagA, channelFlagB := 0, 1
		if nodeA == pubKey2 {
			channelFlagA, channelFlagB = 1, 0
		}

		outPolicy1 := &models.ChannelEdgePolicy1{
			ChannelID:    1000,
			ChannelFlags: lnwire.ChanUpdateChanFlags(channelFlagA),
			ToNode:       nodeB,
		}
		inPolicy1 := &models.ChannelEdgePolicy1{
			ChannelID:    1000,
			ChannelFlags: lnwire.ChanUpdateChanFlags(channelFlagB),
			ToNode:       nodeA,
		}
		node := &node{
			pubKey:   nodeA,
			features: lnwire.EmptyFeatureVector(),
			edgeInfos: []*models.ChannelEdgeInfo1{{
				ChannelID: 1000,
				// Those are direction independent!
				NodeKey1Bytes: pubKey1,
				NodeKey2Bytes: pubKey2,
				Capacity:      500,
			}},
			outPolicies: []*models.ChannelEdgePolicy1{outPolicy1},
			inPolicies:  []*models.ChannelEdgePolicy1{inPolicy1},
		}
		cache := NewGraphCache(10)
		require.NoError(t, cache.AddNode(nil, node))

		var fromChannels, toChannels []*DirectedChannel
		_ = cache.ForEachChannel(nodeA, func(c *DirectedChannel) error {
			fromChannels = append(fromChannels, c)
			return nil
		})
		_ = cache.ForEachChannel(nodeB, func(c *DirectedChannel) error {
			toChannels = append(toChannels, c)
			return nil
		})

		require.Len(t, fromChannels, 1)
		require.Len(t, toChannels, 1)

		require.Equal(t, outPolicy1 != nil, fromChannels[0].OutPolicySet)
		assertCachedPolicyEqual(t, inPolicy1, fromChannels[0].InPolicy)

		require.Equal(t, inPolicy1 != nil, toChannels[0].OutPolicySet)
		assertCachedPolicyEqual(t, outPolicy1, toChannels[0].InPolicy)

		// Now that we've inserted two nodes into the graph, check that
		// we'll recover the same set of channels during ForEachNode.
		nodes := make(map[route.Vertex]struct{})
		chans := make(map[uint64]struct{})
		_ = cache.ForEachNode(func(node route.Vertex,
			edges map[uint64]*DirectedChannel) error {

			nodes[node] = struct{}{}
			for chanID := range edges {
				chans[chanID] = struct{}{}
			}

			return nil
		})

		require.Len(t, nodes, 2)
		require.Len(t, chans, 1)
	}

	runTest(pubKey1, pubKey2)
	runTest(pubKey2, pubKey1)
}

func assertCachedPolicyEqual(t *testing.T, original models.ChannelEdgePolicy,
	cached *models.CachedEdgePolicy) {

	ogFwd := original.ForwardingPolicy()

	require.Equal(t, original.SCID().ToUint64(), cached.ChannelID)
	require.Equal(t, ogFwd.HasMaxHTLC, cached.HasMaxHTLC)
	require.Equal(t, original.IsDisabled(), cached.IsDisabled)
	require.Equal(t, ogFwd.TimeLockDelta, cached.TimeLockDelta)
	require.Equal(t, ogFwd.MinHTLC, cached.MinHTLC)
	require.Equal(t, ogFwd.MaxHTLC, cached.MaxHTLC)
	require.Equal(t, ogFwd.BaseFee, cached.FeeBaseMSat)
	require.Equal(
		t, route.Vertex(original.GetToNode()), cached.ToNodePubKey(),
	)
}
