package main

import (
	"io"
	"net/http"
)

func Client(w http.ResponseWriter, r *http.Request) {
	html := `<!doctype html>
<html>
<head>
  <meta http-equiv="Content-Type" content="text/html; charset=utf-8" />
  <title>golang websocket chatroom</title>
  <script src="https://ajax.googleapis.com/ajax/libs/jquery/1.7.2/jquery.min.js"></script>
  <script>
      var ws = new WebSocket("ws://127.0.0.1:6611/chatroom");
      ws.onopen = function(e){
          console.log("onopen");
          console.dir(e);
      };
      ws.onmessage = function(e){
          console.log("onmessage");
          console.dir(e);
          $('#log').append('<p>'+e.data+'<p>');
          $('#log').get(0).scrollTop = $('#log').get(0).scrollHeight;
      };
      ws.onclose = function(e){
          console.log("onclose");
          console.dir(e);
      };
      ws.onerror = function(e){
          console.log("onerror");
          console.dir(e);
      };
      $(function(){
          $('#msgform').submit(function(){
              ws.send($('#msg').val()+"\n");
              $('#log').append('<p style="color:red;">My > '+$('#msg').val()+'<p>');
              $('#log').get(0).scrollTop = $('#log').get(0).scrollHeight;
              $('#msg').val('');
              return false;
          });
      });
  </script>
</head>
<body>
  <div id="log" style="height: 300px;overflow-y: scroll;border: 1px solid #CCC;">
  </div>
  <div>
      <form id="msgform">
          <input type="text" id="msg" size="60" />
      </form>
  </div>
</body>
</html>`
	io.WriteString(w, html)
}
