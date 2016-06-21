package reign

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

type Stop struct{}
type B struct{ int }
type C struct{ int }
type D struct{ int }
type FakeMailbox struct{}

func (f FakeMailbox) getID() AddressID {
	return mailboxID(1337)
}

func (f FakeMailbox) send(_ interface{}) error {
	return nil
}

func (f FakeMailbox) notifyAddressOnTerminate(_ Address) {}

func (f FakeMailbox) removeNotifyAddress(_ Address) {}

var anything = func(i interface{}) bool {
	return true
}

func TestRegisterTypeCoverage(t *testing.T) {
	RegisterType(t)
}

func TestMailboxReceiveNext(t *testing.T) {
	connections, _ := noClustering(NullLogger)

	a, m := connections.NewMailbox()
	defer m.Terminate()

	msgs := make(chan interface{})

	done := make(chan bool)
	go func() {
		for {
			msg := m.ReceiveNext()
			if _, ok := msg.(Stop); ok {
				done <- true
				return
			}
			msgs <- msg
		}
	}()

	a.Send("hello")
	received := <-msgs

	if received.(string) != "hello" {
		t.Fatal("Did not receive the expected value")
	}

	// this tests that messages can stack up, the goroutine above blocks on
	// trying to send the first one out on the chan
	a.Send("1")
	a.Send("2")
	a.Send("3")
	a.Send(Stop{})

	received = <-msgs
	if received.(string) != "1" {
		t.Fatal("Did not receive the 1")
	}
	received = <-msgs
	if received.(string) != "2" {
		t.Fatal("Did not receive the 2")
	}
	received = <-msgs
	if received.(string) != "3" {
		t.Fatal("Did not receive the 3")
	}

	<-done
}

func TestMailboxReceive(t *testing.T) {
	connections, _ := noClustering(NullLogger)

	a, m := connections.NewMailbox()
	defer m.Terminate()

	msgs := make(chan interface{})
	matches := make(chan func(interface{}) bool)
	done := make(chan bool)

	// this channel and the next few funcs allow us to control the
	// syncronization enough to verify some properties of the Receive
	// function, by ensuring the controlling goroutine (this func) can be
	// sure we've progressed into the part of the Receive function that we
	// expect. Note this is for sync only, the value is NOT the value of
	// the match itself.
	matching := make(chan bool)

	matchC := func(i interface{}) (ok bool) {
		_, ok = i.(C)
		matching <- true
		return
	}
	matchStop := func(i interface{}) (ok bool) {
		_, ok = i.(Stop)
		matching <- true
		return
	}

	go func() {
		for {
			matcher := <-matches
			msg := m.Receive(matcher)
			if _, ok := msg.(Stop); ok {
				done <- true
				return
			}
			msgs <- msg
		}
	}()

	b := B{1}
	c := C{2}
	d := D{3}

	// to keep track of what the mailbox should have, we'll keep a list in
	// the comments here:
	a.Send(b)
	a.Send(c)
	a.Send(d)

	// contains: [b, c, d]
	matches <- matchC
	// must permit one use of the match function per searched item
	<-matching
	// should find C on this match
	<-matching
	msg := <-msgs

	if _, ok := msg.(C); !ok {
		t.Fatal("Did not retrieve the correct message")
	}

	if !reflect.DeepEqual(m.messages, []message{{b}, {d}}) {
		t.Fatal("Did not properly fix up the message queue")
	}

	// now test the case where we don't have the message we want
	waitingDone := make(chan bool)
	go func() {
		matches <- matchC
		msg := <-msgs
		if _, ok := msg.(C); !ok {
			t.Fatal("Did not retrieve the correct message")
		}
		waitingDone <- true
	}()

	// will run two matches against what is already there (b, d), then wait
	<-matching
	<-matching
	// Receive() will be at m.cond.Wait() at this point, allowing us to lock
	// the mutex and send d.
	a.Send(d)
	// Pick up reading from where we left off.  We won't match on d.
	<-matching
	a.Send(c)
	// We will match on c we just sent.
	<-matching
	<-waitingDone

	if !reflect.DeepEqual(m.messages, []message{{b}, {d}, {d}}) {
		t.Fatal("Did not properly fix up the message queue")
	}

	// Send the Stop message before sending matchStop.  Otherwise, we deadlock with Receive()
	// holding the mutex, Send() attempting to acquire the mutex, and matchStop blocking on
	// its write to the matching channel.
	//
	// Alternatively, we could send matchStop, read from matching 3 times to progress Receive()
	// to m.cond.Wait().  This would release the mutex so Send() can acquire it and deliver the
	// Stop message.  The subsequent read from matching will successfully match on the Stop message.
	a.Send(Stop{})
	matches <- matchStop
	// match against the three messages in the queue
	<-matching
	<-matching
	<-matching
	// then successfully match the Stop we put on...
	<-matching
	// at which point we're done.
	<-done
}

func TestBasicTerminate(t *testing.T) {
	connections, _ := noClustering(NullLogger)

	addr1, mailbox1 := connections.NewMailbox()
	addr2, mailbox2 := connections.NewMailbox()
	defer mailbox2.Terminate()

	addr1.NotifyAddressOnTerminate(addr2)

	mailbox1.Terminate()
	// double-termination is legal
	mailbox1.Terminate()

	msg := mailbox2.ReceiveNext()
	if msg.(MailboxTerminated).(mailboxID) != addr1.GetID() {
		t.Fatal("Terminate did not send the right termination message")
	}

	err := addr1.Send("message")
	if err != ErrMailboxTerminated {
		t.Fatal("Sending to a closed mailbox does not yield the terminated error")
	}

	addr1.NotifyAddressOnTerminate(addr2)
	msg = mailbox2.ReceiveNext()
	if msg.(MailboxTerminated).(mailboxID) != addr1.GetID() {
		t.Fatal("Terminate did not send the right termination message for terminated mailbox")
	}

	terminatedResult := mailbox1.ReceiveNext()
	if terminatedResult.(MailboxTerminated).(mailboxID) != addr1.GetID() {
		t.Fatal("ReceiveNext from a terminated mailbox does not return MailboxTerminated properly")
	}

	addr1S, mailbox1S := connections.NewMailbox()
	mailbox1S.Terminate()
	terminatedResult = mailbox1S.Receive(anything)
	if terminatedResult.(MailboxTerminated).(mailboxID) != addr1S.GetID() {
		t.Fatal("Receive from a terminated mailbox does not return MailboxTerminated properly")
	}

}

func TestAsyncTerminateOnReceive(t *testing.T) {
	connections, _ := noClustering(NullLogger)

	wantHello := func(i interface{}) bool {
		iReal, isStr := i.(string)
		if !isStr {
			return false
		}
		return iReal == "Hello"
	}

	addr1, mailbox1 := connections.NewMailbox()

	// This goroutine uses private variables watches to see when the first
	// message has been sent. The Receive call is not looking for this message,
	// so once we see that len(m.messages) is no longer 0, we know that the
	// Receive call is in the for loop part of the call.
	// Once that happens, this will Terminate the mailbox.
	go func() {
		// FIXME: klunky, not sure how to get this to be guaranteed that the Receive
		// is in the for loop without klunking up the implementation...
		time.Sleep(5 * time.Millisecond)
		mailbox1.cond.L.Lock()
		for len(mailbox1.messages) != 1 {
			mailbox1.cond.Wait()
		}
		mailbox1.cond.L.Unlock()
		mailbox1.Terminate()
	}()

	// And here, we run a Receive call that won't match the first message
	// we send it, and assert that it gets the correct MailboxTerminated.
	var result interface{}
	done := make(chan struct{})

	go func() {
		result = mailbox1.Receive(wantHello)
		done <- struct{}{}
	}()

	addr1.Send(1)

	<-done

	// The end result of all this setup is that we should be able to show
	// that the .Receive call ended up with a MailboxTerminated as its result
	if result.(mailboxID) != addr1.GetID() {
		t.Fatal("Terminating the Receive on Terminate doesn't work")
	}
}

func TestAsyncTerminateOnReceiveNext(t *testing.T) {
	connections, _ := noClustering(NullLogger)

	addr1, mailbox1 := connections.NewMailbox()

	// Similar to the previous test, except simpler
	go func() {
		time.Sleep(5 * time.Millisecond)
		mailbox1.Terminate()
	}()

	// And here, we run a Receive call that won't match the first message
	// we send it, and assert that it gets the correct MailboxTerminated.
	var result interface{}
	done := make(chan struct{})

	go func() {
		result = mailbox1.ReceiveNext()
		done <- struct{}{}
	}()

	<-done

	// The end result of all this setup is that we should be able to show
	// that the .Receive call ended up with a MailboxTerminated as its result
	if result.(mailboxID) != addr1.GetID() {
		t.Fatal("Terminating the ReceiveNext on Terminate doesn't work")
	}
}

func TestGetAddress(t *testing.T) {
	connections, _ := noClustering(NullLogger)
	a, _ := connections.NewMailbox()
	// Make an invalid node ID
	a.id = mailboxID(1337)
	a.mailbox = nil
	if !panics(func() { a.getAddress() }) {
		t.Fatal("does not panic when getting a remotemailbox from a node that doesn't exist")
	}

	a.connectionServer = nil
	if !panics(func() { a.getAddress() }) {
		t.Fatal("does not panic when attempting to get the address of an Address with no" +
			"connectionServer")
	}
}

func TestRemoveOfNotifications(t *testing.T) {
	connections, _ := noClustering(NullLogger)

	addr, mailbox1 := connections.NewMailbox()
	addr2, mailbox2 := connections.NewMailbox()

	// no crashing
	addr.RemoveNotifyAddress(addr2)

	addr.NotifyAddressOnTerminate(addr2)
	addr.RemoveNotifyAddress(addr2)
	if len(addr.getAddress().(*Mailbox).notificationAddresses) != 0 {
		t.Fatal("Removing addresses doesn't work as expected")
	}

	mailbox1.Terminate()
	mailbox2.Terminate()
}

func TestSendByID(t *testing.T) {
	connections, _ := noClustering(NullLogger)

	_, mailbox := connections.NewMailbox()
	defer mailbox.Terminate()

	// Verify that creating a new address with the same ID and connectionServer works
	var addr Address
	addr.id = mailbox.id
	addr.connectionServer = mailbox.parent.connectionServer
	err := addr.Send("Hello")

	msg := mailbox.ReceiveNext()
	str, isStr := msg.(string)

	if err != nil || !isStr || str != "Hello" {
		t.Fatal("sendByID failed:", msg)
	}

	addr = Address{}
	addr.id = mailboxID(256) + mailboxID(connections.ThisNode.ID)
	addr.connectionServer = mailbox.parent.connectionServer
	err = addr.Send("Hello")
	if err != ErrMailboxTerminated {
		t.Fatal("sendByID happily sent to a terminated mailbox")
	}
}

func getMarshalsAndTest(a address) ([]byte, []byte, string) {
	addr := Address{a.getID(), nil, a}
	bin, err := addr.MarshalBinary()
	if err != nil {
		panic("fail to marshal binary")
	}

	text, err := addr.MarshalText()
	if err != nil {
		panic("fail to marshal text")
	}

	s := addr.String()

	// If unmarshalling doesn't go through handleIncomingConnections, no connectionServer will
	// be put into the Address or its internal registryMailbox. So we only compare the names
	var addrBin Address
	err = addrBin.UnmarshalBinary(bin)
	if err != nil {
		panic("Could not unmarshal the marshaled bin: " + string(bin))
	}
	binID := addrBin.GetID()
	binFailed := false
	switch binID.(type) {
	case mailboxID:
		if binID != addr.GetID() {
			binFailed = true
		}
	case registryMailbox:
		if binID.(registryMailbox).name != addr.GetID().(registryMailbox).name {
			binFailed = true
		}
	}
	if binFailed {
		panic("After unmarshaling the bin, ids are not ==")
	}

	var addrText Address
	err = addrText.UnmarshalText(text)
	if err != nil {
		panic("could not unmarshal the text")
	}
	textID := addrText.GetID()
	textFailed := false
	switch textID.(type) {
	case mailboxID:
		if textID != addr.GetID() {
			textFailed = true
		}
	case registryMailbox:
		if textID.(registryMailbox).name != addr.GetID().(registryMailbox).name {
			textFailed = true
		}
	}
	if textFailed {
		fmt.Printf("%#v %#v %#v\n", bin, addrText.GetID(), addr.GetID())
		panic(fmt.Sprintf("After unmarshalling the text, ids are not ==: %#v %#v", addrText.GetID(), addr.GetID()))
	}

	return bin, text, s
}

func TestMarshaling(t *testing.T) {
	connections, _ := noClustering(NullLogger)

	a, _ := connections.NewMailbox()
	a.mailbox = FakeMailbox{}
	_, err := a.MarshalBinary()
	if err != ErrIllegalAddressFormat {
		t.Fatal("Address with invalid mailbox did not error on binary marshal")
	}

	mID := mailboxID(257)
	connections.ThisNode.ID = mID.NodeID()
	mailbox := &Mailbox{id: mID}
	bin, text, s := getMarshalsAndTest(mailbox)
	if !reflect.DeepEqual(bin, []byte{60, 0x81, 0x02}) {
		t.Fatal("mailboxID did not binary marshal as expected")
	}
	if string(text) != "<1:1>" {
		t.Fatal("mailboxID failed to marshal to text " + string(text))
	}
	if s != "<1:1>" {
		t.Fatal("mailboxID failed to String properly")
	}

	bra := boundRemoteAddress{mailboxID(257), nil}
	bin, text, s = getMarshalsAndTest(bra)
	if !reflect.DeepEqual(bin, []byte{60, 0x81, 0x02}) {
		t.Fatal("bra did not binary marshal as expected")
	}
	if string(text) != "<1:1>" {
		t.Fatal("bra failed to marshal to text")
	}
	if s != "<1:1>" {
		t.Fatal("bra failed to String properly")
	}

	bin, text, s = getMarshalsAndTest(noMailbox{})
	if !reflect.DeepEqual(bin, []byte("X")) {
		t.Fatal("noMailbox did not binary marshal as expected")
	}
	if string(text) != "X" {
		t.Fatal("noMailbox did not text marshal as expected")
	}
	if s != "X" {
		t.Fatal("noMailbox did not String as expected")
	}

	bin, text, s = getMarshalsAndTest(registryMailbox{"A", connections})
	if !reflect.DeepEqual(bin, []byte("\"A")) {
		t.Fatal("registryMailbox did not binary marshal as expected")
	}
	if string(text) != "\"A\"" {
		t.Fatal("registryMailbox did not text marshal as expected: " + string(text))
	}
	if s != "\"A\"" {
		t.Fatal("registryMailbox did not string as expected")
	}
}

func TestUnmarshalAddressErrors(t *testing.T) {
	a := &Address{}

	err := a.UnmarshalBinary([]byte{})
	if err == nil {
		t.Fatal("Can unmarshal an address from 0 bytes?")
	}
	err = a.UnmarshalBinary([]byte{0xFF})
	if err == nil {
		t.Fatal("Can unmarshal an address from a blantently illegal value")
	}

	err = a.UnmarshalText(nil)
	if err == nil {
		t.Fatal("Can unmarshal an address from nil bytes?")
	}
	for _, addrText := range []string{
		"somethingreallylongthatcan'tpossiblybeanaddress",
		"1:1>",
		"<1:1",
		"<1111>",
		"<a:1>",
		"<1:a>",
		"<258:1>",
		"<0:72057594037927937>",
		"<0:7205759403792793599>",
		"<-1:-1>",
	} {
		err = a.UnmarshalText([]byte(addrText))
		if err == nil {
			t.Fatal("Can unmarshal into an address:", addrText)
		}
	}
}

func TestCoverNoMailbox(t *testing.T) {
	mID := mailboxID(257)
	nm := noMailbox{mID}

	if nm.send(939) != ErrMailboxTerminated {
		t.Fatal("Can send to the no mailbox somehow")
	}
	if nm.getID() != mID {
		t.Fatal("getID incorrectly implemented for noMailbox")
	}
	nm.notifyAddressOnTerminate(Address{mID, nil, nm})
	nm.removeNotifyAddress(Address{mID, nil, nm})

	// FIXME: Test marshal/unmarshal
}

func TestCoverCanBeRegistered(t *testing.T) {
	mID := mailboxID(1)
	if !mID.canBeGloballyRegistered() {
		t.Fatal("Can't register mailboxIDs globally")
	}

	rm := registryMailbox{"", nil}
	if rm.canBeGloballyRegistered() {
		t.Fatal("Can globally register registry mailboxes")
	}

	nm := noMailbox{}
	if nm.canBeGloballyRegistered() {
		t.Fatal("Can globally register noMailboxes")
	}

	var bra boundRemoteAddress
	if bra.canBeGloballyRegistered() {
		t.Fatal("Can globally register boundRemoteAddresses")
	}
}

// Cover the errors not tested by anything else
func TestCoverAddressMarshaling(t *testing.T) {
	//connections, _ := noClustering(NullLogger)
	var a Address

	b, err := a.MarshalBinary()
	if b != nil || err != ErrIllegalAddressFormat {
		t.Fatal("Wrong error from marshaling binary of empty address #%v, #%v", b, err)
	}

	a.clearAddress()
	err = a.UnmarshalBinary([]byte("<"))
	if err != ErrIllegalAddressFormat {
		t.Fatal("Wrong error from unmarshaling illegal binary mailbox")
	}

	a.clearAddress()
	rm := registryMailbox{"hello", a.connectionServer}
	a.id = rm
	if a.getAddress() != rm {
		t.Fatal("Can't unmarshal an Address from a registryMailbox")
	}

	connections, _ := noClustering(NullLogger)
	a, m := connections.NewMailbox()

	defer m.Terminate()

	a2, _ := connections.NewMailbox()
	a2.UnmarshalFromID(a.GetID())
	a2.Send("test")

	msg := m.ReceiveNext()
	if !reflect.DeepEqual(msg, "test") {
		t.Fatal("Can't unmarshal a local address from an ID correctly.")
	}

	err = a.UnmarshalText([]byte("<23456789012345678901234"))
	if err != ErrIllegalAddressFormat {
		t.Fatal("fails the length check on normal mailboxes")
	}
	err = a.UnmarshalText([]byte("\"moo"))
	if err != ErrIllegalAddressFormat {
		t.Fatal("fails to properly check registry mailboxes in text for quotes")
	}

	a = Address{}
	b, err = a.MarshalText()
	if err == nil {
		t.Fatal("can marshal nonexistant address to Text")
	}
}
