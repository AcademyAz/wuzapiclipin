package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"net/http"
	"strconv"
	"strings"

	"github.com/vincent-petithory/dataurl"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type statusSendRequest struct {
	Type     string `json:"type"`
	File     string `json:"file"`
	Text     string `json:"text"`
	ID       string `json:"id"`
	MimeType string `json:"mimetype"`
}

const statusRequestMaxBytes int64 = (fetchImageMaxBytes * 4 / 3) + (1024 * 1024)

func loadStatusImage(ctx context.Context, source string) ([]byte, string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, "", errors.New("file is required for image status")
	}

	var data []byte
	if strings.HasPrefix(strings.ToLower(source), "data:image/") {
		decoded, err := dataurl.DecodeString(source)
		if err != nil {
			return nil, "", fmt.Errorf("invalid image data URL: %w", err)
		}
		data = decoded.Data
	} else if isHTTPURL(source) {
		remoteData, _, err := fetchURLBytes(ctx, source, fetchImageMaxBytes)
		if err != nil {
			return nil, "", fmt.Errorf("could not download status image: %w", err)
		}
		data = remoteData
	} else {
		return nil, "", errors.New("file must be an HTTP(S) URL or data:image URL")
	}

	if len(data) == 0 {
		return nil, "", errors.New("status image is empty")
	}
	if int64(len(data)) > fetchImageMaxBytes {
		return nil, "", fmt.Errorf("status image exceeds allowed size (%d bytes)", fetchImageMaxBytes)
	}

	detectedMimeType := http.DetectContentType(data)
	if !strings.HasPrefix(detectedMimeType, "image/") {
		return nil, "", fmt.Errorf("unsupported image content type %q", detectedMimeType)
	}
	return data, detectedMimeType, nil
}

func validateStatusMimeType(detectedMimeType string, explicitMimeType string) error {
	explicitMimeType = strings.ToLower(strings.TrimSpace(strings.Split(explicitMimeType, ";")[0]))
	if explicitMimeType == "" {
		return nil
	}
	if !strings.HasPrefix(explicitMimeType, "image/") {
		return fmt.Errorf("mimetype must be an image type, got %q", explicitMimeType)
	}

	detectedMimeType = strings.ToLower(strings.TrimSpace(detectedMimeType))
	if explicitMimeType == "image/jpg" {
		explicitMimeType = "image/jpeg"
	}
	if explicitMimeType != detectedMimeType {
		return fmt.Errorf("mimetype %q does not match detected content type %q", explicitMimeType, detectedMimeType)
	}
	return nil
}

func buildStatusImageMessage(
	data []byte,
	mimeType string,
	caption string,
	uploaded whatsmeow.UploadResponse,
) (*waE2E.Message, error) {
	decodedImage, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("could not decode status image: %w", err)
	}

	thumbnail, err := jpegThumbnail(decodedImage, 72, 72)
	if err != nil {
		return nil, fmt.Errorf("could not create status thumbnail: %w", err)
	}

	return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption:       proto.String(strings.TrimSpace(caption)),
		URL:           proto.String(uploaded.URL),
		DirectPath:    proto.String(uploaded.DirectPath),
		MediaKey:      uploaded.MediaKey,
		Mimetype:      proto.String(mimeType),
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(data))),
		JPEGThumbnail: thumbnail,
	}}, nil
}

// SendStatus publishes an ephemeral WhatsApp status (story).
// This is different from SetStatusMessage, which updates the profile "About" text.
func (s *server) SendStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userInfo := r.Context().Value("userinfo").(Values)
		txtid := userInfo.Get("Id")
		client := clientManager.GetWhatsmeowClient(txtid)
		if client == nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("no session"))
			return
		}

		var request statusSendRequest
		r.Body = http.MaxBytesReader(w, r.Body, statusRequestMaxBytes)
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			s.Respond(w, r, http.StatusBadRequest, errors.New("could not decode Payload"))
			return
		}

		statusType := strings.ToLower(strings.TrimSpace(request.Type))
		if statusType == "" {
			s.Respond(w, r, http.StatusBadRequest, errors.New("missing type in Payload"))
			return
		}
		if statusType != "image" {
			s.Respond(w, r, http.StatusBadRequest, fmt.Errorf("unsupported status type %q; currently supported: image", request.Type))
			return
		}

		imageData, mimeType, err := loadStatusImage(r.Context(), request.File)
		if err != nil {
			s.Respond(w, r, http.StatusBadRequest, err)
			return
		}
		if err := validateStatusMimeType(mimeType, request.MimeType); err != nil {
			s.Respond(w, r, http.StatusBadRequest, err)
			return
		}

		uploaded, err := client.Upload(r.Context(), imageData, whatsmeow.MediaImage)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, fmt.Errorf("failed to upload status image: %w", err))
			return
		}

		message, err := buildStatusImageMessage(imageData, mimeType, request.Text, uploaded)
		if err != nil {
			s.Respond(w, r, http.StatusBadRequest, err)
			return
		}

		messageID := strings.TrimSpace(request.ID)
		if messageID == "" {
			messageID = client.GenerateMessageID()
		}

		response, err := client.SendMessage(
			r.Context(),
			types.StatusBroadcastJID,
			message,
			whatsmeow.SendRequestExtra{ID: messageID},
		)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, fmt.Errorf("failed to send image status: %w", err))
			return
		}

		historyLimit, _ := strconv.Atoi(userInfo.Get("History"))
		s.saveOutgoingMessageToHistory(
			txtid,
			types.StatusBroadcastJID.String(),
			messageID,
			"status_image",
			request.Text,
			"",
			historyLimit,
		)
		s.publishSentMessageEvent(
			userInfo.Get("Token"),
			txtid,
			txtid,
			types.StatusBroadcastJID,
			messageID,
			message,
			response.Timestamp,
			"status_image",
		)

		responseJSON, _ := json.Marshal(map[string]interface{}{
			"Details":   "Sent",
			"Timestamp": response.Timestamp.Unix(),
			"Id":        messageID,
			"Type":      statusType,
		})
		s.Respond(w, r, http.StatusOK, string(responseJSON))
	}
}
