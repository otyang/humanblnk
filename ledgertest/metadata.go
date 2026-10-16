package ledgertest

import "github.com/otyang/humanblnk/blnk"

func (s *Server) updateMetadata(id string, md blnk.MetaData) (blnk.MetaData, error) {
	var target *blnk.MetaData
	switch {
	case s.balances[id] != nil:
		target = &s.balances[id].MetaData
	case s.txns[id] != nil:
		target = &s.txns[id].MetaData
	case s.identities[id] != nil:
		target = &s.identities[id].MetaData
	case s.ledgers[id] != nil:
		target = &s.ledgers[id].MetaData
	default:
		return nil, fail(404, blnk.CodeMetadataEntityNotFound, "entity %s not found", id)
	}
	if *target == nil {
		*target = blnk.MetaData{}
	}
	for k, v := range md {
		(*target)[k] = v
	}
	return *target, nil
}
