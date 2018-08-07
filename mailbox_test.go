package reign

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

type Stop struct{}
type B struct{ int }
type C struct{ int }
type D struct{ int }
type FakeMailbox struct{}

func (f FakeMailbox) getMailboxID() MailboxID {
	return MailboxID(1337)
}

func (f FakeMailbox) send(_ interface{}) error {
	return nil
}

func (f FakeMailbox) canBeGloballyRegistered() bool {
	return false
}

func (f FakeMailbox) canBeGloballyUnregistered() bool {
	return false
}

func (f FakeMailbox) notifyAddressOnTerminate(_ *Address) {}

func (f FakeMailbox) removeNotifyAddress(_ *Address) {}

var anything = func(i interface{}) bool {
	return true
}

func TestRegisterTypeCoverage(t *testing.T) {
	RegisterType(t)
}

func TestMailboxReceiveNext(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

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

	if err := a.Send("hello"); err != nil {
		t.Fatal()
	}
	received := <-msgs

	if received.(string) != "hello" {
		t.Fatal("Did not receive the expected value")
	}

	// this tests that messages can stack up, the goroutine above blocks on
	// trying to send the first one out on the chan
	if err := a.Send("1"); err != nil {
		t.Fatal(err)
	}
	if err := a.Send("2"); err != nil {
		t.Fatal(err)
	}
	if err := a.Send("3"); err != nil {
		t.Fatal(err)
	}
	if err := a.Send(Stop{}); err != nil {
		t.Fatal(err)
	}

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

func TestMailboxReceiveNextAsync(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

	a, m := connections.NewMailbox()
	defer m.Terminate()

	msgs := []string{"1", "2", "3"}
	for _, msg := range msgs {
		if err := a.Send(msg); err != nil {
			t.Fatal(err)
		}
	}

	for _, msg := range msgs {
		recv, ok := m.ReceiveNextAsync()
		if !ok {
			t.Fatalf("Did not receive a message. Expected '%s'", msg)
		}
		if recv != msg {
			t.Fatalf("Received incorrect message. Expected '%s', got '%s'", msg, recv)
		}
	}
	_, ok := m.ReceiveNextAsync()
	if ok {
		t.Fatal("ReceiveNextAsync() on an empty mailbox should have failed")
	}
}

func TestMailboxReceiveNextTimeout(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

	a, m := connections.NewMailbox()
	defer m.Terminate()

	msgs := []string{"1", "2", "3"}
	for _, msg := range msgs {
		if err := a.Send(msg); err != nil {
			t.Fatal(err)
		}
	}

	for _, msg := range msgs {
		recv, ok := m.ReceiveNextTimeout(timeout)
		if !ok {
			t.Fatalf("Did not receive a message. Expected '%s'", msg)
		}
		if recv != msg {
			t.Fatalf("Received incorrect message. Expected '%s', got '%s'", msg, recv)
		}
	}

	// Now test that we actually wait for at least as long as the timeout specifies
	startTime := time.Now()
	_, ok := m.ReceiveNextTimeout(timeout)
	endTime := time.Now()
	expectedEndTime := startTime.Add(timeout)
	if ok {
		t.Fatal("ReceiveNextAsync() on an empty mailbox should have failed")
	}
	if expectedEndTime.After(endTime) {
		t.Fatal("Timed out too soon")
	}
}

func TestMailboxReceive(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

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
	if err := a.Send(b); err != nil {
		t.Fatal(err)
	}
	if err := a.Send(c); err != nil {
		t.Fatal(err)
	}
	if err := a.Send(d); err != nil {
		t.Fatal(err)
	}

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
	fatal := make(chan struct{})
	go func() {
		matches <- matchC
		msg := <-msgs
		if _, ok := msg.(C); !ok {
			close(fatal)
		}
		waitingDone <- true
	}()

	match := func() {
		select {
		case <-matching:
		case <-fatal:
			t.Fatal("Did not retrieve the correct message")
		}
	}

	// will run two matches against what is already there (b, d), then wait
	match()
	match()
	// Receive() will be at m.cond.Wait() at this point, allowing us to lock
	// the mutex and send d.
	if err := a.Send(d); err != nil {
		t.Fatal(err)
	}
	// Pick up reading from where we left off.  We won't match on d.
	match()
	if err := a.Send(c); err != nil {
		t.Fatal(err)
	}
	// We will match on c we just sent.
	match()
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
	if err := a.Send(Stop{}); err != nil {
		t.Fatal(err)
	}
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

func TestSimpleMailboxTerminate(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

	addr, mailbox := connections.NewMailbox()

	// Make certain the mailbox is empty.
	m, ok := mailbox.ReceiveNextAsync()
	if ok {
		t.Fatalf("Received an unexpected message: %#v", m)
	}

	// Deliver a message to the mailbox.
	err := addr.Send("Hello")
	if err != nil {
		t.Fatal(err)
	}

	// The mailbox currently has one message in it.  However, we should
	// only receive the MailboxTerminated message after we terminate the
	// mailbox, *not* the "Hello" message.
	mailbox.Terminate()
	m, ok = mailbox.ReceiveNextAsync()
	if _, terminated := m.(MailboxTerminated); !ok || !terminated {
		t.Fatalf("Expected a MailboxTerminated message: %#v", m)
	}
}

func TestTerminate(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

	addr1, mailbox1 := connections.NewMailbox()
	addr2, mailbox2 := connections.NewMailbox()
	defer mailbox2.Terminate()

	addr1.NotifyAddressOnTerminate(addr2)

	mailbox1.Terminate()
	// double-termination is legal
	mailbox1.Terminate()

	msg, ok := mailbox2.ReceiveNextAsync()
	if !ok {
		t.Fatal("No message received. Expected termination.")
	}
	if MailboxID(msg.(MailboxTerminated)) != addr1.mailboxID {
		t.Fatal("Terminate did not send the right termination message")
	}

	if err := addr1.Send("message"); err != ErrMailboxTerminated {
		t.Fatal("Sending to a closed mailbox does not yield the terminated error")
	}

	addr1.NotifyAddressOnTerminate(addr2)
	msg, ok = mailbox2.ReceiveNextAsync()
	if !ok {
		t.Fatal("No message received. Expected termination.")
	}
	if MailboxID(msg.(MailboxTerminated)) != addr1.mailboxID {
		t.Fatal("Terminate did not send the right termination message for terminated mailbox")
	}

	terminatedResult, ok := mailbox1.ReceiveNextAsync()
	if !ok {
		t.Fatal("No message received. Expected termination.")
	}
	if MailboxID(terminatedResult.(MailboxTerminated)) != addr1.mailboxID {
		t.Fatal("ReceiveNextAsync  from a terminated mailbox does not return MailboxTerminated properly")
	}

	addr1S, mailbox1S := connections.NewMailbox()
	mailbox1S.Terminate()
	terminatedResult = mailbox1S.Receive(anything)
	if MailboxID(terminatedResult.(MailboxTerminated)) != addr1S.mailboxID {
		t.Fatal("Receive from a terminated mailbox does not return MailboxTerminated properly")
	}
}

func TestAsyncTerminateOnReceive(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

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

	if err := addr1.Send(1); err != nil {
		t.Fatal(err)
	}

	<-done

	// The end result of all this setup is that we should be able to show
	// that the .Receive call ended up with a MailboxTerminated as its result
	if MailboxID(result.(MailboxTerminated)) != addr1.mailboxID {
		t.Fatal("Terminating the Receive on Terminate doesn't work")
	}
}

func TestAsyncTerminateOnReceiveNext(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

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
	if MailboxID(result.(MailboxTerminated)) != addr1.mailboxID {
		t.Fatal("Terminating the ReceiveNext on Terminate doesn't work")
	}
}

func TestGetAddress(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

	a, m := connections.NewMailbox()
	defer m.Terminate()

	a2 := Address{mailboxID: a.mailboxID}
	a2.getAddress()
	if a2.connectionServer == nil {
		t.Fatal("failed to get connection server with call to getAddress()")
	}

	// Make an invalid node ID
	a.mailboxID = MailboxID(1337)
	a.mailbox = nil
	if !panics(func() { a.getAddress() }) {
		t.Fatal("does not panic when getting a remotemailbox from a node that doesn't exist")
	}

	a.connectionServer = nil
	if !panics(func() { a.getAddress() }) {
		t.Fatal("does not panic when attempting to get the address of an Address with no connectionServer")
	}
}

func TestRemoveOfNotifications(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

	addr, mailbox1 := connections.NewMailbox()
	defer mailbox1.Terminate()

	addr2, mailbox2 := connections.NewMailbox()
	defer mailbox2.Terminate()

	// no crashing
	addr.RemoveNotifyAddress(addr2)

	addr.NotifyAddressOnTerminate(addr2)
	addr.RemoveNotifyAddress(addr2)
	if len(addr.getAddress().(*Mailbox).notificationAddresses) != 0 {
		t.Fatal("Removing addresses doesn't work as expected")
	}
}

func TestSendByID(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

	_, mailbox := connections.NewMailbox()
	defer mailbox.Terminate()

	// Verify that creating a new address with the same ID works
	addr := Address{mailboxID: mailbox.id}
	err := addr.Send("Hello")

	msg, ok := mailbox.ReceiveNextAsync()
	if !ok {
		t.Fatal("No message received")
	}
	str, isStr := msg.(string)

	if err != nil || !isStr || str != "Hello" {
		t.Fatal("sendByID failed:", msg)
	}

	addr = Address{mailboxID: MailboxID(256) + MailboxID(connections.ThisNode.ID)}
	err = addr.Send("Hello")
	if err != ErrMailboxTerminated {
		t.Fatal("sendByID happily sent to a terminated mailbox")
	}
}

func getMarshalsAndTest(a address, t *testing.T) ([]byte, []byte, []byte, string) {
	addr := Address{a.getMailboxID(), a, nil}
	bin, err := addr.MarshalBinary()
	if err != nil {
		t.Fatal("fail to marshal binary")
	}

	text, err := addr.MarshalText()
	if err != nil {
		t.Fatal("fail to marshal text")
	}

	json, err := addr.MarshalJSON()
	if err != nil {
		t.Fatal("fail to marshal JSON")
	}

	s := addr.String()

	// If unmarshalling doesn't go through handleIncomingConnections, no connectionServer will
	// be put into the Address or its internal registryMailbox. So we only compare the names
	var addrBin Address
	err = addrBin.UnmarshalBinary(bin)
	if err != nil {
		t.Fatalf("Could not unmarshal the marshaled bin: %#x", bin)
	}
	binID := addrBin.mailboxID
	if binID != addr.mailboxID {
		t.Fatalf("After unmarshaling the bin, ids are not equal: left = %v, right = %v", binID, addr.mailboxID)
	}

	var addrText Address
	err = addrText.UnmarshalText(text)
	if err != nil {
		t.Fatal("could not unmarshal the text")
	}
	textID := addrText.mailboxID
	if textID != addr.mailboxID {
		t.Fatalf("%#v %#v %#v\nAfter unmarshalling the text, ids are not ==: %#v %#v",
			bin, addrText.mailboxID, addr.mailboxID, addrText.mailboxID, addr.mailboxID)
	}

	var addrJSON Address
	err = addrJSON.UnmarshalJSON(json)
	if err != nil {
		t.Fatalf("could not unmarshal the JSON %q: %s", json, err)
	}
	jsonID := addrJSON.mailboxID
	if jsonID != addr.mailboxID {
		t.Fatalf("%#v %#v %#v\nAfter unmarshalling the JSON, ids are not ==: %#v %#v",
			bin, addrJSON.mailboxID, addr.mailboxID, addrJSON.mailboxID, addr.mailboxID)
	}

	return bin, text, json, s
}

func TestMarshaling(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

	a, m := connections.NewMailbox()
	defer m.Terminate()

	a.mailbox = FakeMailbox{}
	_, err := a.MarshalBinary()
	if err != ErrIllegalAddressFormat {
		t.Fatal("Address with invalid mailbox did not error on binary marshal")
	}
	mID := MailboxID(257)
	connections.ThisNode.ID = mID.NodeID()
	mailbox := &Mailbox{id: mID}
	bin, text, json, s := getMarshalsAndTest(mailbox, t)
	if !reflect.DeepEqual(bin, []byte{60, 0x81, 0x02}) {
		t.Error("mailboxID did not binary marshal as expected")
	}
	if string(text) != "<1:1>" {
		t.Error("mailboxID failed to marshal to text " + string(text))
	}
	if string(json) != "<1:1>" {
		t.Error("mailboxID failed to marshal to JSON " + string(json))
	}
	if s != "<1:1>" {
		t.Error("mailboxID failed to String properly")
	}

	bra := boundRemoteAddress{MailboxID: MailboxID(257)}
	bin, text, json, s = getMarshalsAndTest(bra, t)
	if !reflect.DeepEqual(bin, []byte{60, 0x81, 0x02}) {
		t.Error("bra did not binary marshal as expected")
	}
	if string(text) != "<1:1>" {
		t.Error("bra failed to marshal to text")
	}
	if string(json) != "<1:1>" {
		t.Error("bra failed to marshal to JSON")
	}
	if s != "<1:1>" {
		t.Error("bra failed to String properly")
	}

	bin, text, json, s = getMarshalsAndTest(noMailbox{}, t)
	if !reflect.DeepEqual(bin, []byte("X")) {
		t.Error("noMailbox did not binary marshal as expected")
	}
	if string(text) != "X" {
		t.Error("noMailbox did not text marshal as expected")
	}
	if string(json) != "X" {
		t.Error("noMailbox did not JSON marshal as expected")
	}
	if s != "X" {
		t.Error("noMailbox did not String as expected")
	}
}

func TestAddressUnmarshalJSON(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

	addr1, _ := cs.NewMailbox()
	addr1Bytes, err := addr1.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	addr2 := &Address{}
	err = addr2.UnmarshalJSON(addr1Bytes)
	if err != nil {
		t.Fatal(err)
	}

	// This will fail unless the Address has its mailbox cached.
	_, err = json.Marshal(addr2)
	if err != nil {
		t.Fatal(err)
	}
}

func TestUnmarshalAddressErrors(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

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
	mID := MailboxID(257)
	nm := noMailbox{mID}

	if nm.send(939) != ErrMailboxTerminated {
		t.Fatal("Can send to the no mailbox somehow")
	}
	if nm.MailboxID != mID {
		t.Fatal("mailboxID incorrectly implemented for noMailbox")
	}
	nm.notifyAddressOnTerminate(&Address{mailboxID: mID, mailbox: nm})
	nm.removeNotifyAddress(&Address{mailboxID: mID, mailbox: nm})

	// FIXME: Test marshal/unmarshal
}

func TestCoverNoConnections(t *testing.T) {
	setConnections(nil)

	if !panics(func() { connections.NewMailbox() }) {
		t.Fatal("Mailboxes can be created without connections")
	}
}

func TestCoverCanBeRegistered(t *testing.T) {
	mbox := Mailbox{}
	if !mbox.canBeGloballyRegistered() {
		t.Fatal("Can't register mailboxIDs globally")
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
	cs, r := noClustering(NullLogger)
	defer cs.Terminate()
	defer r.Terminate()

	a := &Address{}
	a.connectionServer = cs

	b, err := a.MarshalBinary()
	if b != nil || err != ErrIllegalAddressFormat {
		t.Fatalf("Wrong error from marshaling binary of empty address #%v, #%v", b, err)
	}

	a = &Address{}
	err = a.UnmarshalBinary([]byte("<"))
	if err != ErrIllegalAddressFormat {
		t.Fatal("Wrong error from unmarshaling illegal binary mailbox")
	}

	a, m1 := connections.NewMailbox()
	defer m1.Terminate()

	a2, m2 := connections.NewMailbox()
	defer m2.Terminate()

	a2.UnmarshalFromID(a.mailboxID)
	a2.connectionServer = connections
	err = a2.Send("test")
	if err != nil {
		t.Fatal(err)
	}

	msg, ok := m1.ReceiveNextAsync()
	if !ok {
		t.Fatal("Mailbox received nothing")
	}
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

	a = &Address{}
	_, err = a.MarshalText()
	if err == nil {
		t.Fatal("can marshal nonexistant address to Text")
	}
}
