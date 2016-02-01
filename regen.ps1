$ProtocDir = ".\packages\Google.Protobuf.3.0.0-alpha4\tools"
$Tools = ".\packages\Grpc.Tools.0.12.0\tools"
$ProtoDir = ".\Paymetheus.Rpc\protos"
$ToolsInclude = ".\packages\Google.ProtocolBuffers.2.4.1.555\tools"
$OutDir = "Paymetheus.Rpc"

$Target = "api.proto"

& $ProtocDir\protoc.exe -I $ProtoDir -I $ToolsInclude --csharp_out=$OutDir --grpc_out=$OutDir --plugin=protoc-gen-grpc=$Tools\grpc_csharp_plugin.exe $ProtoDir\$Target
