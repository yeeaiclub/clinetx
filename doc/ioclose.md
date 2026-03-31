> 如果**不写这两行重建 Body**，就会出现“重试时 body 为空”的情况，对吧？

是的，**正是这样**，原因可以概括成两步：

1. **`req.Body` 是一次性流**  
   - 第一次 `client.Do(req)` 时，`net/http` 会把 `req.Body` 从当前位置读到 EOF，把数据发给服务端。  
   - 读完之后，`req.Body` 内部光标已经在结尾了，不会自动回到开头。

2. **如果不重建新的 Body，后续重试再读就是空**  
   - 后续第二次、第三次 `client.Do(req)` 还去读同一个 `req.Body`，它已经在 EOF，自然读出来就是空。  
   - 这就是你最初例子里我们用 `readOnlyBody` 包一层后，第二次服务端收到空字符串的原因。

而现在这两行：

```go
attemptReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
attemptReq.ContentLength = int64(len(bodyBytes))
```

做的事情就是：

- 用我们**提前缓存好的 `bodyBytes`**，每次重试构造一个“全新的可读流”，相当于每次都重新给 `net/http` 一个完整的 body。
- 这样哪怕原始的 `req.Body` 已经被读完并关闭了，重试时仍然能发出完整的请求体，就不会再出现你说的那种“后面几次 body 为空”的情况了。