$Tools = ".\packages\Grpc.Tools.0.15.0\tools\windows_x64"
$ProtoDir = ".\Paymetheus.Rpc\protos"
$OutDir = "Paymetheus.Rpc"

$Target = "api.proto"

& $Tools\protoc.exe -I $ProtoDir --csharp_out=$OutDir --grpc_out=$OutDir `
	--plugin=protoc-gen-grpc=$Tools\grpc_csharp_plugin.exe $ProtoDir\$Target
