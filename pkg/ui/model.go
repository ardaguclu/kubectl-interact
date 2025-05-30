// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ui

import (
	"io"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"slices"
	"sync"
)

type Document struct {
	mutex         sync.Mutex
	subscriptions []*subscription
	nextID        uint64

	streams genericiooptions.IOStreams

	blocks []Block
}

func (d *Document) Blocks() []Block {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	return d.blocks
}

func (d *Document) NumBlocks() int {
	return len(d.Blocks())
}

func (d *Document) IndexOf(find Block) int {
	blocks := d.Blocks()

	for i, b := range blocks {
		if b == find {
			return i
		}
	}
	return -1
}

func NewDocument(streams genericiooptions.IOStreams) *Document {
	return &Document{
		nextID:  1,
		streams: streams,
	}
}

type Block interface {
	attached(doc *Document)

	Document() *Document
}

type Subscriber interface {
	DocumentChanged(doc *Document, block Block, streams genericiooptions.IOStreams)
}

type subscription struct {
	doc        *Document
	id         uint64
	subscriber Subscriber
}

func (s *subscription) Close() error {
	s.doc.mutex.Lock()
	defer s.doc.mutex.Unlock()
	s.subscriber = nil
	return nil
}

func (d *Document) AddSubscription(subscriber Subscriber) io.Closer {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	id := d.nextID
	d.nextID++

	s := &subscription{
		doc:        d,
		id:         id,
		subscriber: subscriber,
	}

	newSubscriptions := make([]*subscription, 0, len(d.subscriptions)+1)
	for _, s := range d.subscriptions {
		if s == nil || s.subscriber == nil {
			continue
		}
		newSubscriptions = append(newSubscriptions, s)
	}
	newSubscriptions = append(newSubscriptions, s)
	d.subscriptions = newSubscriptions
	return s
}

func (d *Document) sendDocumentChanged(b Block, streams genericiooptions.IOStreams) {
	d.mutex.Lock()
	subscriptions := d.subscriptions
	d.mutex.Unlock()

	for _, s := range subscriptions {
		if s == nil || s.subscriber == nil {
			continue
		}

		s.subscriber.DocumentChanged(d, b, streams)
	}
}

func (d *Document) AddBlock(block Block, streams genericiooptions.IOStreams) {
	d.mutex.Lock()

	// Copy-on-write to minimize locking
	newBlocks := slices.Clone(d.blocks)
	newBlocks = append(newBlocks, block)
	d.blocks = newBlocks

	block.attached(d)

	d.mutex.Unlock()

	d.sendDocumentChanged(block, streams)
}

func (d *Document) blockChanged(block Block, streams genericiooptions.IOStreams) {
	if d == nil {
		return
	}

	d.sendDocumentChanged(block, streams)
}
