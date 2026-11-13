package sqlite

import (
	"context"
	"fmt"

	"dconnector/internal/core/model"
)

// ---- Estadísticas de historial de la cuenta (interfaz opcional service.HistoryStatsStore) ----

// ChatHistoryStats devuelve cuántos mensajes hay guardados de un chat y cuál es el más antiguo
// (el ancla para pedir historial anterior). Sin mensajes: Count 0 y Oldest vacío.
func (s *AccountStore) ChatHistoryStats(ctx context.Context, chatNativeID string) (model.ChatHistoryStats, error) {
	var st model.ChatHistoryStats
	var newest int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(MAX(ts), 0) FROM messages WHERE chat_native_id = ?`, chatNativeID).
		Scan(&st.Count, &newest); err != nil {
		return st, fmt.Errorf("almacén: no se pudo contar los mensajes del chat: %w", err)
	}
	st.NewestTs = newest
	if st.Count == 0 {
		return st, nil
	}
	var fromMe int
	err := s.db.QueryRowContext(ctx,
		`SELECT native_id, sender_native_id, from_me, ts FROM messages
		 WHERE chat_native_id = ? ORDER BY ts ASC, native_id ASC LIMIT 1`, chatNativeID).
		Scan(&st.Oldest.NativeID, &st.Oldest.SenderNativeID, &fromMe, &st.Oldest.Timestamp)
	if err != nil {
		return st, fmt.Errorf("almacén: no se pudo leer el mensaje más antiguo: %w", err)
	}
	st.Oldest.ChatNativeID, st.Oldest.FromMe = chatNativeID, fromMe != 0
	return st, nil
}

// HistoryTotals devuelve cuántos mensajes tiene guardados la cuenta y el ts (ms) del más antiguo.
func (s *AccountStore) HistoryTotals(ctx context.Context) (int, int64, error) {
	var n int
	var oldest int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(MIN(ts), 0) FROM messages`).Scan(&n, &oldest); err != nil {
		return 0, 0, fmt.Errorf("almacén: no se pudo contar los mensajes de la cuenta: %w", err)
	}
	return n, oldest, nil
}
