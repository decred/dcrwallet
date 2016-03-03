$ProtocDir = ".\packages\Google.Protobuf.3.0.0-beta2\tools"
$Tools = ".\packages\Grpc.Tools.0.13.0\tools"
$ProtoDir = ".\Paymetheus.Rpc\protos"
$OutDir = "Paymetheus.Rpc"

$Target = "api.proto"

& $ProtocDir\protoc.exe -I $ProtoDir --csharp_out=$OutDir --grpc_out=$OutDir `
	--plugin=protoc-gen-grpc=$Tools\grpc_csharp_plugin.exe $ProtoDir\$Target
