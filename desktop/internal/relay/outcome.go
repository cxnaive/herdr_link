package relay

import "errors"

type RequestError struct {
	Unknown bool
	Message string
}

func (e *RequestError) Error() string { return e.Message }

// UnknownOutcome treats transport failures conservatively. Only a definite
// rejection allows a write to be submitted again without checking its result.
func UnknownOutcome(err error) bool {
	if err == nil {
		return false
	}
	var result *RequestError
	if errors.As(err, &result) {
		return result.Unknown
	}
	return true
}
func commandReply(reply Message) (Message, error) {
	phase := Text(reply, "phase")
	if phase == "dispatched_unknown" || phase == "unconfirmed" || phase == "awaiting_evidence" {
		return nil, &RequestError{true, "操作结果未确认，请先核对当前 Agent"}
	}
	if Text(reply, "type") == "error" || reply["ok"] == false || reply["error"] != nil && reply["error"] != "" {
		return nil, &RequestError{false, requestFailureMessage(reply["error"])}
	}
	return unwrap(reply), nil
}

func requestFailureMessage(value any) string {
	code, _ := value.(string)
	if details := Object(value); details != nil {
		code = Text(details, "code")
	}
	switch code {
	case "attachment_unknown_mime":
		return "当前 Relay 不支持此类附件，请更新电脑端 Relay 后重试"
	case "attachment_file_too_large":
		return "附件超过电脑端允许的单文件大小"
	case "attachment_batch_too_large":
		return "附件总大小超过电脑端限制"
	case "attachment_batch_limit":
		return "附件数量超过电脑端限制"
	case "attachment_invalid_name":
		return "附件名称无效，请重命名后重试"
	case "attachment_upload_expired":
		return "上传已过期，请重新添加附件"
	case "attachment_upload_busy":
		return "电脑端正在处理其他上传，请稍后重试"
	case "attachment_upload_state_unknown":
		return "附件上传中断或校验未通过，请重新添加附件"
	case "attachment_upload_unavailable":
		return "电脑端的附件上传暂不可用"
	case "attachment_upload_failed":
		return "附件未能保存到电脑端，请检查 Relay 状态后重试"
	}
	if code == "" {
		return "请求未完成"
	}
	return "请求未完成：" + code
}
