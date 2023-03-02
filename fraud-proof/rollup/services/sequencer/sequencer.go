package sequencer

import (
	ethc "github.com/ethereum/go-ethereum/common"
	"github.com/mantlenetworkio/mantle/l2geth/core/rawdb"
	"github.com/mantlenetworkio/mantle/l2geth/p2p"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/mantlenetworkio/mantle/fraud-proof/bindings"
	"github.com/mantlenetworkio/mantle/fraud-proof/proof"
	"github.com/mantlenetworkio/mantle/fraud-proof/rollup/services"
	rollupTypes "github.com/mantlenetworkio/mantle/fraud-proof/rollup/types"
	"github.com/mantlenetworkio/mantle/l2geth/common"
	"github.com/mantlenetworkio/mantle/l2geth/log"
)

func RegisterService(eth services.Backend, proofBackend proof.Backend, cfg *services.Config, auth *bind.TransactOpts) {
	sequencer, err := New(eth, proofBackend, cfg, auth)
	if err != nil {
		log.Crit("Failed to register the Rollup service", "err", err)
	}
	sequencer.Start()
	log.Info("Sequencer registered")
}

type challengeCtx struct {
	challengeAddr common.Address
	assertion     *rollupTypes.Assertion
	parent        *rollupTypes.Assertion
}

// Sequencer run confirming loop and respond challenge, assumes no Berlin+London fork on L2
type Sequencer struct {
	*services.BaseService

	confirmedIDCh         chan *big.Int
	challengeCh           chan *challengeCtx
	challengeResolutionCh chan struct{}

	confirmations uint64
}

func New(eth services.Backend, proofBackend proof.Backend, cfg *services.Config, auth *bind.TransactOpts) (*Sequencer, error) {
	base, err := services.NewBaseService(eth, proofBackend, cfg, auth)
	if err != nil {
		return nil, err
	}
	s := &Sequencer{
		BaseService:           base,
		confirmedIDCh:         make(chan *big.Int, 4096),
		challengeCh:           make(chan *challengeCtx),
		challengeResolutionCh: make(chan struct{}),
		confirmations:         cfg.L1Confirmations,
	}
	return s, nil
}

// This goroutine tries to confirm created assertions
func (s *Sequencer) confirmationLoop() {
	defer s.Wg.Done()

	// Watch AssertionCreated event
	createdCh := make(chan *bindings.RollupAssertionCreated, 4096)
	createdSub, err := s.Rollup.Contract.WatchAssertionCreated(&bind.WatchOpts{Context: s.Ctx}, createdCh)
	if err != nil {
		log.Error("Failed to watch rollup event", "err", err)
	}
	defer createdSub.Unsubscribe()

	// Watch AssertionConfirmed event
	confirmedCh := make(chan *bindings.RollupAssertionConfirmed, 4096)
	confirmedSub, err := s.Rollup.Contract.WatchAssertionConfirmed(&bind.WatchOpts{Context: s.Ctx}, confirmedCh)
	if err != nil {
		log.Error("Failed to watch rollup event", "err", err)
	}
	defer confirmedSub.Unsubscribe()

	// Watch L1 blockchain for confirmation period
	headCh := make(chan *types.Header, 4096)
	headSub, err := s.L1.SubscribeNewHead(s.Ctx, headCh)
	if err != nil {
		log.Error("Failed to watch l1 chain head", "err", err)
	}
	defer headSub.Unsubscribe()

	challengedCh := make(chan *bindings.RollupAssertionChallenged, 4096)
	challengedSub, err := s.Rollup.Contract.WatchAssertionChallenged(&bind.WatchOpts{Context: s.Ctx}, challengedCh)
	if err != nil {
		log.Error("Failed to watch rollup event", "err", err)
	}
	defer challengedSub.Unsubscribe()
	isInChallenge := rawdb.ReadFPInChallenge(s.BaseService.ProofBackend.ChainDb())
	for {
		if isInChallenge {
			// Waif for the challenge resolved
			select {
			case <-s.challengeResolutionCh:
				log.Info("Sequencer finished challenge, reset isInChallenge status")
				isInChallenge = false
				rawdb.WriteFPInChallenge(s.BaseService.ProofBackend.ChainDb(), isInChallenge)
			case <-s.Ctx.Done():
				return
			}
		} else {
			select {
			case ev := <-createdCh:
				// New assertion created on L1 Rollup
				log.Info("Get New Assertion...", "AssertionID", ev.AssertionID,
					"AsserterAddr", ev.AsserterAddr, "VmHash", ev.VmHash, "InboxSize", ev.InboxSize)
			case header := <-headCh:
				// todo : optimization the check with block height
				// Get confirm block header
				if s.confirmations != 0 {
					num := new(big.Int)
					num.SetUint64(s.confirmations)
					num.Sub(header.Number, num)
					header, err = s.L1.HeaderByNumber(s.Ctx, num)
					if err != nil {
						log.Error("Failed to get confirmed header", "err", err)
						continue
					}
				}
				// Get first unresolved confirm assertion and check deadline
				lastRSAID, err := s.Rollup.LastResolvedAssertionID()
				if err != nil {
					log.Error("Failed to get last resolved assertion ID", "err", err)
					continue
				}
				lastCAID, err := s.Rollup.LastCreatedAssertionID()
				if err != nil {
					log.Error("Failed to get last created assertion ID", "err", err)
					continue
				}
				if lastCAID.Uint64() <= lastRSAID.Uint64() {
					continue
				}

				// New block mined on L1
				log.Info("Sequencer sync new layer1 block", "height", header.Number)
				firstUnresolvedID := lastRSAID.Uint64() + 1
				firstUnconfirmedAssertion, err := s.AssertionMap.Assertions(new(big.Int).SetUint64(firstUnresolvedID))
				if err != nil {
					log.Error("Failed to get first unresolved Assertion", "err", err, "ID", firstUnresolvedID)
					continue
				}
				if header.Time >= firstUnconfirmedAssertion.Deadline.Uint64() {
					// Confirmation period has past, confirm it
					log.Info("Sequencer call ConfirmFirstUnresolvedAssertion...")
					log.Info("Current pendingAssertion", "id", firstUnresolvedID)
					_, err := s.Rollup.ConfirmFirstUnresolvedAssertion()
					if err != nil {
						log.Error("Failed to confirm DA", "err", err)
						continue
					}
				}

			case ev := <-confirmedCh:
				log.Info("New confirmed assertion", "id", ev.AssertionID)
			case ev := <-challengedCh:
				log.Warn("New challenge rise!!!!!!", "ev", ev)
				// New challenge raised
				//if ev.AssertionID.Cmp(pendingAssertion.ID) == 0 {
				//	s.challengeCh <- &challengeCtx{
				//		common.Address(ev.ChallengeAddr),
				//		pendingAssertion,
				//	}
				//	isInChallenge = true
				//}

				// todo when interrupt at this moment, check staker`s status
				challengeAssertion := new(rollupTypes.Assertion)
				perent := new(rollupTypes.Assertion)
				if ret, err := s.AssertionMap.Assertions(ev.AssertionID); err != nil {
					log.Crit("Get assertion failed", "id", ev.AssertionID, "err", err)
				} else {
					challengeAssertion.ID = ev.AssertionID
					challengeAssertion.VmHash = ret.StateHash
					challengeAssertion.InboxSize = ret.InboxSize
					challengeAssertion.Parent = ret.Parent
					challengeAssertion.Deadline = ret.Deadline
					challengeAssertion.ProposalTime = ret.ProposalTime
				}
				if ret, err := s.AssertionMap.Assertions(challengeAssertion.Parent); err != nil {
					log.Crit("Get assertion failed", "id", challengeAssertion.Parent, "err", err)
				} else {
					perent.ID = ev.AssertionID
					perent.VmHash = ret.StateHash
					perent.InboxSize = ret.InboxSize
					perent.Parent = ret.Parent
					perent.Deadline = ret.Deadline
					perent.ProposalTime = ret.ProposalTime
				}

				s.challengeCh <- &challengeCtx{
					common.Address(ev.ChallengeAddr),
					challengeAssertion,
					perent,
				}
				isInChallenge = true
				rawdb.WriteFPInChallenge(s.ProofBackend.ChainDb(), isInChallenge)
			case <-s.Ctx.Done():
				return
			}
		}
	}
}

func (s *Sequencer) challengeLoop() {
	defer s.Wg.Done()

	// Watch L1 blockchain for challenge timeout
	headCh := make(chan *types.Header, 4096)
	headSub, err := s.L1.SubscribeNewHead(s.Ctx, headCh)
	if err != nil {
		log.Error("Failed to watch l1 chain head", "err", err)
	}
	defer headSub.Unsubscribe()

	var challengeSession *bindings.IChallengeSession
	var states []*proof.ExecutionState

	var bisectedCh chan *bindings.IChallengeBisected
	var bisectedSub event.Subscription
	var challengeCompletedCh chan *bindings.IChallengeChallengeCompleted
	var challengeCompletedSub event.Subscription

	inChallenge := false
	var opponentTimeout uint64

	for {
		if inChallenge {
			select {
			case ev := <-bisectedCh:
				// case get bisection, if is our turn
				//   if in single step, submit proof
				//   if multiple step, track current segment, update
				responder, err := challengeSession.CurrentResponder()
				if err != nil {
					// TODO: error handling
					log.Error("Can not get current responder", "error", err)
					continue
				}
				log.Info("Responder info...", "responder", responder, "staker", s.Config.StakeAddr)
				if common.Address(responder) == s.Config.StakeAddr {
					log.Info("Sequencer start to respond new bisection...")
					// If it's our turn
					//err := services.RespondBisection(s.BaseService, abi, challengeSession, ev, states, common.Hash{}, false)
					err := services.RespondBisection(s.BaseService, challengeSession, ev, states)
					if err != nil {
						// TODO: error handling
						log.Error("Can not respond to bisection", "error", err)
						continue
					}
				} else {
					opponentTimeLeft, err := challengeSession.CurrentResponderTimeLeft()
					if err != nil {
						// TODO: error handling
						log.Error("Can not get current responder left time", "error", err)
						continue
					}
					log.Info("[Sequencer] Opponent time left", "blockTime", ev.BlockTime.Uint64(), "timeLeft", opponentTimeLeft)
					opponentTimeout = ev.BlockTime.Uint64() + opponentTimeLeft.Uint64()
				}
			case header := <-headCh:
				if opponentTimeout == 0 {
					continue
				}
				// TODO: can we use >= here?
				log.Info("New header incoming...", "header.Number", header.Number, "header.Time", header.Time, "opponentTimeout", opponentTimeout)
				if header.Time > opponentTimeout {
					_, err = challengeSession.Timeout()
					if err != nil {
						log.Error("Can not timeout opponent", "error", err)
						continue
						// TODO: wait some time before retry
						// TODO: fix race condition
					}
					log.Info("Timeout challenge...")
				}
			case ev := <-challengeCompletedCh:
				// TODO: handle if we are not winner --> state corrupted
				log.Info("[challenge] Challenge completed", "winner", ev.Winner)
				bisectedSub.Unsubscribe()
				challengeCompletedSub.Unsubscribe()
				states = []*proof.ExecutionState{}
				inChallenge = false
				s.challengeResolutionCh <- struct{}{}
			case <-s.Ctx.Done():
				bisectedSub.Unsubscribe()
				challengeCompletedSub.Unsubscribe()
				return
			}
		} else {
			select {
			case ctx := <-s.challengeCh:
				log.Warn("Sequencer receive new challenge!!!", "handle it", ctx)
				challenge, err := bindings.NewIChallenge(ethc.Address(ctx.challengeAddr), s.L1)
				if err != nil {
					log.Crit("Failed to access ongoing challenge", "address", ctx.challengeAddr, "err", err)
				}
				challengeSession = &bindings.IChallengeSession{
					Contract:     challenge,
					CallOpts:     bind.CallOpts{Pending: true, Context: s.Ctx},
					TransactOpts: *s.TransactOpts,
				}
				bisectedCh = make(chan *bindings.IChallengeBisected, 4096)
				bisectedSub, err = challenge.WatchBisected(&bind.WatchOpts{Context: s.Ctx}, bisectedCh)
				if err != nil {
					log.Crit("Failed to watch challenge event", "err", err)
				}
				challengeCompletedCh = make(chan *bindings.IChallengeChallengeCompleted, 4096)
				challengeCompletedSub, err = challenge.WatchChallengeCompleted(&bind.WatchOpts{Context: s.Ctx}, challengeCompletedCh)
				if err != nil {
					log.Crit("Failed to watch challenge event", "err", err)
				}
				log.Info("Sequencer generate state...")
				states, err = proof.GenerateStates(
					s.ProofBackend,
					s.Ctx,
					ctx.parent.InboxSize.Uint64(),
					ctx.assertion.InboxSize.Uint64(),
					nil,
				)
				if err != nil {
					log.Crit("Failed to generate states", "err", err)
				}
				log.Info("Sequencer generate state end...")

				// todo: check weather already initialized
				numSteps := uint64(len(states)) - 1
				log.Info("Print generated states", "states[0]", states[0].Hash().String(), "states[numSteps]", states[numSteps].Hash().String())
				_, err = challengeSession.InitializeChallengeLength(services.MidState(states, 0, numSteps), new(big.Int).SetUint64(numSteps))
				if err != nil {
					log.Crit("Failed to initialize challenge", "err", err)
				}
				inChallenge = true
			case <-headCh:
				continue // consume channel values
			case <-s.Ctx.Done():
				return
			}
		}
	}
}

func (s *Sequencer) Protocols() []p2p.Protocol {
	// TODO: sequencer APIs
	return []p2p.Protocol{}
}

func (s *Sequencer) APIs() []rpc.API {
	// TODO: sequencer APIs
	return []rpc.API{}
}

func (s *Sequencer) Start() error {
	_ = s.BaseService.Start(true, true)

	s.Wg.Add(2)
	go s.confirmationLoop()
	go s.challengeLoop()
	log.Info("fraud-proof defender started")
	return nil
}

func (s *Sequencer) Stop() error {
	log.Info("fraud-proof defender stopped")
	s.Cancel()
	s.Wg.Wait()
	return nil
}
