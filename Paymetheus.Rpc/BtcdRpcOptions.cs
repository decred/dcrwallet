// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.IO;

namespace Paymetheus.Rpc
{
    public class BtcdRpcOptions
    {
        static readonly string LocalApplicationDataDirectory = Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData);

        public static readonly string LocalCertificateDirectory = Path.Combine(LocalApplicationDataDirectory, "Btcd");
        public static readonly string LocalCertificateFileName = "rpc.cert";
        public static readonly string LocalCertifiateFilePath = Path.Combine(LocalCertificateDirectory, LocalCertificateFileName);

        public BtcdRpcOptions(string networkAddr, string rpcUser, string rpcPass, string certPath)
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
    }
}
