package json

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"

	"github.com/cometbft/cometbft/libs/bytes"
	cmjson "github.com/cometbft/cometbft/libs/json"

	"github.com/gorilla/rpc/v2"
	"github.com/gorilla/rpc/v2/json2"

	"github.com/cometbft/cometbft/libs/log"
)

type handler struct {
	srv    *service
	mux    *http.ServeMux
	codec  rpc.Codec
	logger log.Logger
}

func newHandler(s *service, codec rpc.Codec, logger log.Logger) *handler {
	mux := http.NewServeMux()
	h := &handler{
		srv:    s,
		mux:    mux,
		codec:  codec,
		logger: logger,
	}

	mux.HandleFunc("/", h.serveJSONRPC)
	mux.HandleFunc("/websocket", h.wsHandler)
	for name, method := range s.methods {
		logger.Debug("registering method", "name", name)
		mux.HandleFunc("/"+name, h.newHandler(method))
	}

	return h
}
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// serveJSONRPC serves HTTP request
func (h *handler) serveJSONRPC(w http.ResponseWriter, r *http.Request) {
	h.serveJSONRPCforWS(w, r, nil)
}

// serveJSONRPC serves HTTP request
// implementation is highly inspired by Gorilla RPC v2 (but simplified a lot)
func (h *handler) serveJSONRPCforWS(w http.ResponseWriter, r *http.Request, wsConn *wsConn) {
	// Create a new codec request.
	codecReq := h.codec.NewRequest(r)
	if wsConn != nil {
		wsConn.codecReq = codecReq
	}
	// Get service method to be called.
	method, err := codecReq.Method()
	if err != nil {
		var e *json2.Error
		if method == "" && errors.As(err, &e) && e.Message == "EOF" {
			// just serve empty page if request is empty
			return
		}
		codecReq.WriteError(w, http.StatusBadRequest, err)
		return
	}
	methodSpec, ok := h.srv.methods[method]
	if !ok {
		codecReq.WriteError(w, int(json2.E_NO_METHOD), err)
		return
	}

	// Decode the args.
	args := reflect.New(methodSpec.argsType)
	if errRead := codecReq.ReadRequest(args.Interface()); errRead != nil {
		codecReq.WriteError(w, http.StatusBadRequest, errRead)
		return
	}

	callArgs := []reflect.Value{
		reflect.ValueOf(r),
		args,
	}
	if methodSpec.ws {
		callArgs = append(callArgs, reflect.ValueOf(wsConn))
	}
	rets := methodSpec.m.Call(callArgs)

	// Extract the result to error if needed.
	var errResult error
	statusCode := http.StatusOK
	errInter := rets[1].Interface()
	if errInter != nil {
		statusCode = http.StatusBadRequest
		errResult = errInter.(error)
	}

	// Prevents Internet Explorer from MIME-sniffing a response away
	// from the declared content-type
	w.Header().Set("x-content-type-options", "nosniff")

	// Encode the response.
	if errResult == nil {
		var raw json.RawMessage
		raw, err = cmjson.Marshal(rets[0].Interface())
		if err != nil {
			codecReq.WriteError(w, http.StatusInternalServerError, err)
			return
		}
		codecReq.WriteResponse(w, raw)
	} else {
		codecReq.WriteError(w, statusCode, errResult)
	}
}

func (h *handler) newHandler(methodSpec *method) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		args := reflect.New(methodSpec.argsType)
		values, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			h.encodeAndWriteResponse(w, nil, err, int(json2.E_PARSE))
			return
		}
		for i := 0; i < methodSpec.argsType.NumField(); i++ {
			field := methodSpec.argsType.Field(i)
			name := field.Tag.Get("json")
			kind := field.Type.Kind()
			// pointers can be skipped from required check
			if kind != reflect.Pointer && !values.Has(name) {
				h.encodeAndWriteResponse(w, nil, fmt.Errorf("missing param '%s'", name), int(json2.E_INVALID_REQ))
				return
			}
			rawVal := values.Get(name)
			var err error
			switch kind {
			case reflect.Pointer:
				err = setPointerParam(rawVal, &args, i)
			case reflect.Bool:
				err = setBoolParam(rawVal, &args, i)
			case reflect.Int, reflect.Int64:
				err = setIntParam(rawVal, &args, i)
			case reflect.String:
				args.Elem().Field(i).SetString(rawVal)
			case reflect.Slice:
				// []byte is a reflect.Slice of reflect.Uint8's
				if field.Type.Elem().Kind() == reflect.Uint8 {
					err = setByteSliceParam(rawVal, &args, i)
				}
			default:
				err = errors.New("unknown type")
			}
			if err != nil {
				err = fmt.Errorf("failed to parse param '%s': %w", name, err)
				h.encodeAndWriteResponse(w, nil, err, int(json2.E_PARSE))
				return
			}
		}
		rets := methodSpec.m.Call([]reflect.Value{
			reflect.ValueOf(r),
			args,
		})

		// Extract the result to error if needed.
		statusCode := http.StatusOK
		errInter := rets[1].Interface()
		if errInter != nil {
			statusCode = int(json2.E_INTERNAL)
			err = errInter.(error)
		}

		h.encodeAndWriteResponse(w, rets[0].Interface(), err, statusCode)
	}
}

func (h *handler) encodeAndWriteResponse(w http.ResponseWriter, result interface{}, errResult error, statusCode int) {
	// Prevents Internet Explorer from MIME-sniffing a response away
	// from the declared content-type
	w.Header().Set("x-content-type-options", "nosniff")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	resp := response{
		Version: "2.0",
		ID:      []byte("-1"),
	}

	if errResult != nil {
		resp.Error = &json2.Error{Code: json2.ErrorCode(statusCode), Data: errResult.Error()}
	} else {
		bytes, err := cmjson.Marshal(result)
		if err != nil {
			resp.Error = &json2.Error{Code: json2.E_INTERNAL, Data: err.Error()}
		} else {
			resp.Result = bytes
		}
	}

	encoder := json.NewEncoder(w)
	err := encoder.Encode(resp)
	if err != nil {
		h.logger.Error("failed to encode RPC response", "error", err)
	}
}

func setPointerParam(rawVal string, args *reflect.Value, i int) error {
	if rawVal == "" {
		return nil
	}
	field := args.Elem().Field(i)
	switch field.Type() {
	case reflect.TypeOf((*BlockNumber)(nil)):
		var bn BlockNumber
		err := bn.UnmarshalJSON([]byte(rawVal))
		if err != nil {
			return err
		}
		args.Elem().Field(i).Set(reflect.ValueOf(&bn))
		return nil
	case reflect.TypeOf((*StrInt64)(nil)):
		val, err := strconv.ParseInt(rawVal, 10, 64)
		if err != nil {
			return err
		}
		strInt64Val := StrInt64(val)
		field.Set(reflect.ValueOf(&strInt64Val))
	case reflect.TypeOf((*StrInt)(nil)):
		val, err := strconv.Atoi(rawVal)
		if err != nil {
			return err
		}
		strIntVal := StrInt(val)
		field.Set(reflect.ValueOf(&strIntVal))
	case reflect.TypeOf((*string)(nil)):
		field.Set(reflect.ValueOf(&rawVal))
	case reflect.TypeOf((*bool)(nil)):
		val, err := strconv.ParseBool(rawVal)
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(&val))
	case reflect.TypeOf((*bytes.HexBytes)(nil)):
		hexBytes, err := hex.DecodeString(rawVal)
		if err != nil {
			return err
		}
		hb := bytes.HexBytes(hexBytes)
		field.Set(reflect.ValueOf(&hb))
	default:
		return fmt.Errorf("unsupported pointer type: %v", field.Type())
	}
	return nil
}

func setBoolParam(rawVal string, args *reflect.Value, i int) error {
	v, err := strconv.ParseBool(rawVal)
	if err != nil {
		return err
	}
	args.Elem().Field(i).SetBool(v)
	return nil
}

func setIntParam(rawVal string, args *reflect.Value, i int) error {
	v, err := strconv.ParseInt(rawVal, 10, 64)
	if err != nil {
		return err
	}
	args.Elem().Field(i).SetInt(v)
	return nil
}

func setByteSliceParam(rawVal string, args *reflect.Value, i int) error {
	b, err := hex.DecodeString(rawVal)
	if err != nil {
		return err
	}
	args.Elem().Field(i).SetBytes(b)
	return nil
}
