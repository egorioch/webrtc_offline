package main

import (
	"time"

	"github.com/deepch/vdk/av"
	"github.com/imdario/mergo"
	"github.com/sirupsen/logrus"
)

// StreamChannelMake check stream exist
func (obj *StorageST) StreamChannelMake(val ChannelST) ChannelST {
	channel := obj.ChannelDefaults
	if err := mergo.Merge(&channel, val); err != nil {
		// Just ignore the default values and continue
		channel = val
		log.WithFields(logrus.Fields{
			"module": "storage",
			"func":   "StreamChannelMake",
			"call":   "mergo.Merge",
		}).Errorln(err.Error())
	}
	//make client's
	channel.clients = make(map[string]ClientST)

	//make last ack
	channel.ack = time.Now().Add(-255 * time.Hour)

	//make signals buffer chain
	channel.signals = make(chan int, 100)
	return channel
}

// StreamChannelRun one stream and lock
func (obj *StorageST) StreamChannelRun(streamID string, channelID string) {
	obj.mutex.Lock()
	defer obj.mutex.Unlock()
	if streamTmp, ok := obj.Streams[streamID]; ok {
		if channelTmp, ok := streamTmp.Channels[channelID]; ok {
			if !channelTmp.runLock {
				channelTmp.runLock = true
				streamTmp.Channels[channelID] = channelTmp
				obj.Streams[streamID] = streamTmp
				go StreamServerRunStreamDo(streamID, channelID)
			}
		}
	}
}

// StreamChannelUnlock unlock status to no lock
func (obj *StorageST) StreamChannelUnlock(streamID string, channelID string) {
	obj.mutex.Lock()
	defer obj.mutex.Unlock()
	if streamTmp, ok := obj.Streams[streamID]; ok {
		if channelTmp, ok := streamTmp.Channels[channelID]; ok {
			channelTmp.runLock = false
			streamTmp.Channels[channelID] = channelTmp
			obj.Streams[streamID] = streamTmp
		}
	}
}

// StreamChannelControl get stream
func (obj *StorageST) StreamChannelControl(key string, channelID string) (*ChannelST, error) {
	obj.mutex.Lock()
	defer obj.mutex.Unlock()
	if streamTmp, ok := obj.Streams[key]; ok {
		if channelTmp, ok := streamTmp.Channels[channelID]; ok {
			return &channelTmp, nil
		}
	}
	return nil, ErrorStreamNotFound
}

// StreamChannelExist check stream exist
func (obj *StorageST) StreamChannelExist(streamID string, channelID string) bool {
	obj.mutex.Lock()
	defer obj.mutex.Unlock()
	if streamTmp, ok := obj.Streams[streamID]; ok {
		if channelTmp, ok := streamTmp.Channels[channelID]; ok {
			channelTmp.ack = time.Now()
			streamTmp.Channels[channelID] = channelTmp
			obj.Streams[streamID] = streamTmp
			return ok
		}
	}
	return false
}

// StreamChannelCodecs get stream codec storage or wait
func (obj *StorageST) StreamChannelCodecs(streamID string, channelID string) ([]av.CodecData, error) {
	for i := 0; i < 100; i++ {
		ret, err := (func() ([]av.CodecData, error) {
			obj.mutex.RLock()
			defer obj.mutex.RUnlock()
			tmp, ok := obj.Streams[streamID]
			if !ok {
				return nil, ErrorStreamNotFound
			}
			channelTmp, ok := tmp.Channels[channelID]
			if !ok {
				return nil, ErrorStreamChannelNotFound
			}
			return channelTmp.codecs, nil
		})()

		if ret != nil || err != nil {
			return ret, err
		}

		time.Sleep(50 * time.Millisecond)
	}
	return nil, ErrorStreamChannelCodecNotFound
}

// StreamChannelStatus change stream status
func (obj *StorageST) StreamChannelStatus(key string, channelID string, val int) {
	obj.mutex.Lock()
	defer obj.mutex.Unlock()
	if tmp, ok := obj.Streams[key]; ok {
		if channelTmp, ok := tmp.Channels[channelID]; ok {
			channelTmp.Status = val
			tmp.Channels[channelID] = channelTmp
			obj.Streams[key] = tmp
		}
	}
}

// StreamChannelCast broadcast stream
func (obj *StorageST) StreamChannelCast(key string, channelID string, val *av.Packet) {
	obj.mutex.Lock()
	defer obj.mutex.Unlock()
	if tmp, ok := obj.Streams[key]; ok {
		if channelTmp, ok := tmp.Channels[channelID]; ok {
			if len(channelTmp.clients) > 0 {
				for _, i2 := range channelTmp.clients {
					if i2.mode == RTSP {
						continue
					}
					if len(i2.outgoingAVPacket) < 1000 {
						i2.outgoingAVPacket <- val
					} else if len(i2.signals) < 10 {
						i2.signals <- SignalStreamStop
					}
				}
				channelTmp.ack = time.Now()
				tmp.Channels[channelID] = channelTmp
				obj.Streams[key] = tmp
			}
		}
	}
}

// StreamChannelCastProxy broadcast stream
func (obj *StorageST) StreamChannelCastProxy(key string, channelID string, val *[]byte) {
	obj.mutex.Lock()
	defer obj.mutex.Unlock()
	if tmp, ok := obj.Streams[key]; ok {
		if channelTmp, ok := tmp.Channels[channelID]; ok {
			if len(channelTmp.clients) > 0 {
				for _, i2 := range channelTmp.clients {
					if i2.mode != RTSP {
						continue
					}
					if len(i2.outgoingRTPPacket) < 1000 {
						i2.outgoingRTPPacket <- val
					} else if len(i2.signals) < 10 {
						i2.signals <- SignalStreamStop
					}
				}
				channelTmp.ack = time.Now()
				tmp.Channels[channelID] = channelTmp
				obj.Streams[key] = tmp
			}
		}
	}
}

// StreamChannelCodecsUpdate update stream codec storage
func (obj *StorageST) StreamChannelCodecsUpdate(streamID string, channelID string, val []av.CodecData, sdp []byte) {
	obj.mutex.Lock()
	defer obj.mutex.Unlock()
	if tmp, ok := obj.Streams[streamID]; ok {
		if channelTmp, ok := tmp.Channels[channelID]; ok {
			channelTmp.codecs = val
			channelTmp.sdp = sdp
			tmp.Channels[channelID] = channelTmp
			obj.Streams[streamID] = tmp
		}
	}
}

// StreamChannelSDP codec storage or wait
func (obj *StorageST) StreamChannelSDP(streamID string, channelID string) ([]byte, error) {
	for i := 0; i < 100; i++ {
		obj.mutex.RLock()
		tmp, ok := obj.Streams[streamID]
		obj.mutex.RUnlock()
		if !ok {
			return nil, ErrorStreamNotFound
		}
		channelTmp, ok := tmp.Channels[channelID]
		if !ok {
			return nil, ErrorStreamChannelNotFound
		}

		if len(channelTmp.sdp) > 0 {
			return channelTmp.sdp, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, ErrorStreamNotFound
}
