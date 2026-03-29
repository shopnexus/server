package chatecho

import (
	"context"
	"encoding/json"
	"log/slog"

	accountmodel "shopnexus-server/internal/module/account/model"
	chatbiz "shopnexus-server/internal/module/chat/biz"
	chatmodel "shopnexus-server/internal/module/chat/model"
	authclaims "shopnexus-server/internal/shared/claims"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

func (h *Handler) HandleWebSocket(c echo.Context) error {
	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return err
	}

	ws, err := h.upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	accountID := claims.Account.ID

	h.clientsMu.Lock()
	h.clients[accountID] = ws
	h.clientsMu.Unlock()

	defer func() {
		h.clientsMu.Lock()
		delete(h.clients, accountID)
		h.clientsMu.Unlock()
	}()

	for {
		_, msgBytes, err := ws.ReadMessage()
		if err != nil {
			slog.Debug("ws read error", "account_id", accountID, "error", err)
			break
		}

		var wsMsg chatmodel.WSMessage
		if err := json.Unmarshal(msgBytes, &wsMsg); err != nil {
			h.sendWSError(ws, "invalid message format")
			continue
		}

		switch wsMsg.Type {
		case chatmodel.WSTypeSendMessage:
			h.handleSendMessage(ws, claims.Account, wsMsg.Data)
		case chatmodel.WSTypeMarkRead:
			h.handleMarkRead(ws, claims.Account, wsMsg.Data)
		default:
			h.sendWSError(ws, "unknown message type")
		}
	}

	return nil
}

func (h *Handler) handleSendMessage(ws *websocket.Conn, account accountmodel.AuthenticatedAccount, data interface{}) {
	dataBytes, _ := json.Marshal(data)
	var req chatmodel.WSSendMessage
	if err := json.Unmarshal(dataBytes, &req); err != nil {
		h.sendWSError(ws, "invalid send_message payload")
		return
	}

	metadata, _ := json.Marshal(req.Metadata)

	msg, err := h.biz.SendMessage(context.Background(), chatbiz.SendMessageParams{
		Account:        account,
		ConversationID: req.ConversationID,
		Type:           req.Type,
		Content:        req.Content,
		Metadata:       metadata,
	})
	if err != nil {
		h.sendWSError(ws, err.Error())
		return
	}

	conv, err := h.biz.GetConversation(context.Background(), req.ConversationID)
	if err != nil {
		h.sendWSError(ws, err.Error())
		return
	}

	recipientID := conv.BuyerID
	if recipientID == account.ID {
		recipientID = conv.SellerID
	}

	outMsg := chatmodel.WSMessage{Type: chatmodel.WSTypeNewMessage, Data: msg}
	h.sendToClient(recipientID, outMsg)
	h.sendToClient(account.ID, outMsg)
}

func (h *Handler) handleMarkRead(ws *websocket.Conn, account accountmodel.AuthenticatedAccount, data interface{}) {
	dataBytes, _ := json.Marshal(data)
	var req chatmodel.WSMarkRead
	if err := json.Unmarshal(dataBytes, &req); err != nil {
		h.sendWSError(ws, "invalid mark_read payload")
		return
	}

	if err := h.biz.MarkRead(context.Background(), chatbiz.MarkReadParams{
		Account:        account,
		ConversationID: req.ConversationID,
	}); err != nil {
		h.sendWSError(ws, err.Error())
		return
	}

	conv, err := h.biz.GetConversation(context.Background(), req.ConversationID)
	if err != nil {
		h.sendWSError(ws, err.Error())
		return
	}

	recipientID := conv.BuyerID
	if recipientID == account.ID {
		recipientID = conv.SellerID
	}

	h.sendToClient(recipientID, chatmodel.WSMessage{
		Type: chatmodel.WSTypeReadReceipt,
		Data: map[string]interface{}{
			"conversation_id": req.ConversationID,
			"reader_id":       account.ID,
		},
	})
}

func (h *Handler) sendToClient(accountID uuid.UUID, msg chatmodel.WSMessage) {
	h.clientsMu.RLock()
	conn, ok := h.clients[accountID]
	h.clientsMu.RUnlock()

	if ok {
		if err := conn.WriteJSON(msg); err != nil {
			slog.Debug("ws write error", "account_id", accountID, "error", err)
		}
	}
}

func (h *Handler) sendWSError(ws *websocket.Conn, message string) {
	ws.WriteJSON(chatmodel.WSMessage{
		Type: chatmodel.WSTypeError,
		Data: map[string]string{"message": message},
	})
}
