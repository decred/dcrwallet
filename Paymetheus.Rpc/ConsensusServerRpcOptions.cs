// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.IO;

namespace Paymetheus.Rpc
{
    public class ConsensusServerRpcOptions
    {
        public const string ApplicationName = "dcrd";

        public ConsensusServerRpcOptions(string networkAddr, string rpcUser, string rpcPass, string certPath)
        {
            NetworkAddress = networkAddr;
            RpcUser = rpcUser;
            RpcPassword = rpcPass;
            CertificatePath = certPath;
        }

        public string NetworkAddress { get; }
        public string RpcUser { get; }
        public string RpcPassword { get; }
        public string CertificatePath { get; }

        public static string LocalCertificateFilePath() =>
            Path.Combine(Portability.LocalAppData(Environment.OSVersion.Platform, ApplicationName), "rpc.cert");
    }
}
