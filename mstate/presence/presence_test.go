package presence_test

import (
	"labix.org/v2/mgo"
	. "launchpad.net/gocheck"
	state "launchpad.net/juju-core/mstate"
	"launchpad.net/juju-core/mstate/presence"
	"launchpad.net/juju-core/testing"
	"strconv"
	stdtesting "testing"
	"time"
)

func TestPackage(t *stdtesting.T) {
	testing.MgoTestPackage(t)
}

var (
	period     = 50 * time.Millisecond
	longEnough = period * 6
)

type PresenceSuite struct {
	testing.MgoSuite
	testing.LoggingSuite
	presence *mgo.Collection
	pings    *mgo.Collection
	state    *state.State
}

var _ = Suite(&PresenceSuite{})

func (s *PresenceSuite) SetUpSuite(c *C) {
	s.LoggingSuite.SetUpSuite(c)
	s.MgoSuite.SetUpSuite(c)
}

func (s *PresenceSuite) TearDownSuite(c *C) {
	s.MgoSuite.TearDownSuite(c)
	s.LoggingSuite.TearDownSuite(c)
}

func (s *PresenceSuite) SetUpTest(c *C) {
	s.LoggingSuite.SetUpTest(c)
	s.MgoSuite.SetUpTest(c)

	db := s.MgoSuite.Session.DB("presence")
	s.presence = db.C("presence")
	s.pings = db.C("presence.pings")

	var err error
	s.state, err = state.Dial(testing.MgoAddr)
	c.Assert(err, IsNil)

	presence.FakeTimeSlot(0)
}

func (s *PresenceSuite) TearDownTest(c *C) {
	s.state.Close()
	s.MgoSuite.TearDownTest(c)
	s.LoggingSuite.TearDownTest(c)

	presence.RealTimeSlot()
	presence.RealPeriod()
}

func assertChange(c *C, watch <-chan presence.Change, want presence.Change) {
	select {
	case got := <-watch:
		if got != want {
			c.Fatalf("watch reported %v, want %v", got, want)
		}
	case <-time.After(500 * time.Millisecond):
		c.Fatalf("watch reported nothing, want %v", want)
	}
}

func assertNoChange(c *C, watch <-chan presence.Change) {
	select {
	case got := <-watch:
		c.Fatalf("watch reported %v, want nothing", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func (s *PresenceSuite) TestWorkflow(c *C) {
	w := presence.NewWatcher(s.presence)
	pa := presence.NewPinger(s.presence, "a")
	pb := presence.NewPinger(s.presence, "b")
	defer w.Stop()
	defer pa.Stop()
	defer pb.Stop()

	c.Assert(w.Alive("a"), Equals, false)
	c.Assert(w.Alive("b"), Equals, false)

	// Buffer one entry to avoid blocking the watcher here.
	cha := make(chan presence.Change, 1)
	chb := make(chan presence.Change, 1)
	w.Add("a", cha)
	w.Add("b", chb)

	// Initial events with current status.
	assertChange(c, cha, presence.Change{"a", false})
	assertChange(c, chb, presence.Change{"b", false})

	w.ForceRefresh()
	assertNoChange(c, cha)
	assertNoChange(c, chb)

	c.Assert(pa.Start(), IsNil)

	w.ForceRefresh()
	assertChange(c, cha, presence.Change{"a", true})
	assertNoChange(c, cha)
	assertNoChange(c, chb)

	//c.Assert(w.Alive("a"), Equals, true)
	//c.Assert(w.Alive("b"), Equals, false)

	// Changes while the channel is out are not observed.
	w.Remove("a", cha)
	assertNoChange(c, cha)
	pa.Kill()
	w.ForceRefresh()
	pa.Start()
	w.ForceRefresh()
	assertNoChange(c, cha)

	// Initial positive event. No refresh needed.
	w.Add("a", cha)
	assertChange(c, cha, presence.Change{"a", true})

	c.Assert(pb.Start(), IsNil)

	w.ForceRefresh()
	assertChange(c, chb, presence.Change{"b", true})
	assertNoChange(c, cha)
	assertNoChange(c, chb)

	c.Assert(pa.Stop(), IsNil)

	w.ForceRefresh()
	assertNoChange(c, cha)
	assertNoChange(c, chb)

	c.Assert(pa.Kill(), IsNil)
	c.Assert(pb.Kill(), IsNil)

	w.ForceRefresh()
	assertChange(c, cha, presence.Change{"a", false})
	assertChange(c, chb, presence.Change{"b", false})

	c.Assert(w.Stop(), IsNil)
}

func (s *PresenceSuite) TestScale(c *C) {
	const N = 1000
	var ps []*presence.Pinger
	defer func() {
		for _, p := range ps {
			p.Stop()
		}
	}()

	c.Logf("Starting %d pingers...", N)
	for i := 0; i < N; i++ {
		p := presence.NewPinger(s.presence, strconv.Itoa(i))
		c.Assert(p.Start(), IsNil)
		ps = append(ps, p)
	}

	c.Logf("Killing odd ones...")
	for i := 1; i < N; i += 2 {
		c.Assert(ps[i].Kill(), IsNil)
	}

	c.Logf("Checking who's still alive...")
	w := presence.NewWatcher(s.presence)
	defer w.Stop()
	w.ForceRefresh()
	ch := make(chan presence.Change)
	for i := 0; i < N; i++ {
		k := strconv.Itoa(i)
		w.Add(k, ch)
		if i%2 == 0 {
			assertChange(c, ch, presence.Change{k, true})
		} else {
			assertChange(c, ch, presence.Change{k, false})
		}
	}
}

func (s *PresenceSuite) TestExpiry(c *C) {
	w := presence.NewWatcher(s.presence)
	p := presence.NewPinger(s.presence, "a")
	defer w.Stop()
	defer p.Stop()

	ch := make(chan presence.Change, 1)
	w.Add("a", ch)
	assertChange(c, ch, presence.Change{"a", false})

	c.Assert(p.Start(), IsNil)
	w.ForceRefresh()
	assertChange(c, ch, presence.Change{"a", true})

	// Still alive in previous slot.
	presence.FakeTimeSlot(1)
	w.ForceRefresh()
	assertNoChange(c, ch)

	// Two last slots are empty.
	presence.FakeTimeSlot(2)
	w.ForceRefresh()
	assertChange(c, ch, presence.Change{"a", false})

	// Already dead so killing isn't noticed.
	p.Kill()
	w.ForceRefresh()
	assertNoChange(c, ch)
}

func (s *PresenceSuite) TestWatchPeriod(c *C) {
	presence.FakePeriod(1)
	presence.RealTimeSlot()

	w := presence.NewWatcher(s.presence)
	p := presence.NewPinger(s.presence, "a")
	defer w.Stop()
	defer p.Stop()

	ch := make(chan presence.Change)
	w.Add("a", ch)
	assertChange(c, ch, presence.Change{"a", false})

	// A single ping.
	c.Assert(p.Start(), IsNil)
	c.Assert(p.Stop(), IsNil)

	// Wait for next periodic refresh.
	time.Sleep(1 * time.Second)
	assertChange(c, ch, presence.Change{"a", true})
}
