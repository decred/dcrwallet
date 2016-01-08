// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Bitcoin.Util;
using System;

namespace Paymetheus.Bitcoin
{
    public sealed class Transaction
    {
        public const int LatestVersion = 1;

        public struct OutPoint
        {
            public OutPoint(Sha256Hash hash, uint index)
            {
                Hash = hash;
                Index = index;
            }

            public Sha256Hash Hash { get; }
            public uint Index { get; }

            public override string ToString() => $"{Hash}:{Index}";

            public bool IsNull() => Index == uint.MaxValue && Hash.Equals(Sha256Hash.Zero);
        }

        public struct Input
        {
            public Input(OutPoint previousOutPoint, byte[] signatureScript, uint sequence)
            {
                if (signatureScript == null)
                    throw new ArgumentNullException(nameof(signatureScript));

                PreviousOutpoint = previousOutPoint;
                SignatureScript = signatureScript;
                Sequence = sequence;
            }

            public OutPoint PreviousOutpoint { get; }
            public byte[] SignatureScript { get; }
            public uint Sequence { get; }

            public int SerializeSize =>
                40 + CompactInt.SerializeSize((ulong)SignatureScript.Length) + SignatureScript.Length;
        }

        public struct Output
        {
            public Output(Amount amount, byte[] pkScript)
            {
                if (pkScript == null)
                    throw new ArgumentNullException(nameof(pkScript));

                Amount = amount;
                PkScript = pkScript;
            }

            public Amount Amount { get; }
            public byte[] PkScript { get; }

            public int SerializeSize =>
                8 + CompactInt.SerializeSize((ulong)PkScript.Length) + PkScript.Length;
        }

        public Transaction(int version, Input[] inputs, Output[] outputs, uint lockTime)
        {
            Version = version;
            Inputs = inputs;
            Outputs = outputs;
            LockTime = lockTime;
        }

        public int Version { get; }
        public Input[] Inputs { get; }
        public Output[] Outputs { get; }
        public uint LockTime { get; }

        public int SerializeSize
        {
            get
            {
                var size = 8 + CompactInt.SerializeSize((ulong)Inputs.Length) +
                    CompactInt.SerializeSize((ulong)Outputs.Length);
                foreach (var input in Inputs)
                {
                    size += input.SerializeSize;
                }
                foreach (var output in Outputs)
                {
                    size += output.SerializeSize;
                }
                return size;
            }
        }

        public static Transaction Deserialize(byte[] rawTransaction)
        {
            if (rawTransaction == null)
                throw new ArgumentNullException(nameof(rawTransaction));

            int version;
            Input[] inputs;
            Output[] outputs;
            uint lockTime;

            var cursor = new ByteCursor(rawTransaction);

            try
            {
                version = cursor.ReadInt32();
                var inputCount = cursor.ReadCompact();
                if (inputCount > TransactionRules.MaxInputs)
                {
                    var reason = $"Input count {inputCount} exceeds maximum value {TransactionRules.MaxInputs}";
                    throw new EncodingException(typeof(Transaction), cursor, reason);
                }
                inputs = new Input[inputCount];
                for (int i = 0; i < (int)inputCount; i++)
                {
                    var previousHash = new Sha256Hash(cursor.ReadBytes(Sha256Hash.Length));
                    var previousIndex = cursor.ReadUInt32();
                    var previousOutPoint = new OutPoint(previousHash, previousIndex);
                    var signatureScript = cursor.ReadVarBytes(TransactionRules.MaxPayload);
                    var sequence = cursor.ReadUInt32();
                    inputs[i] = new Input(previousOutPoint, signatureScript, sequence);
                }
                var outputCount = cursor.ReadCompact();
                if (outputCount > TransactionRules.MaxOutputs)
                {
                    var reason = $"Output count {outputCount} exceeds maximum value {TransactionRules.MaxOutputs}";
                    throw new EncodingException(typeof(Transaction), cursor, reason);
                }
                outputs = new Output[outputCount];
                for (int i = 0; i < (int)outputCount; i++)
                {
                    var amount = (Amount)cursor.ReadInt64();
                    var pkScript = cursor.ReadVarBytes(TransactionRules.MaxPayload);
                    outputs[i] = new Output(amount, pkScript);
                }
                lockTime = cursor.ReadUInt32();
            }
            catch (Exception ex) when (!(ex is EncodingException))
            {
                throw new EncodingException(typeof(Transaction), cursor, ex);
            }

            return new Transaction(version, inputs, outputs, lockTime);
        }

        public byte[] Serialize()
        {
            var destination = new byte[SerializeSize];
            SerializeTo(destination);
            return destination;
        }

        public void SerializeTo(byte[] destination, int offset = 0)
        {
            var cursor = new ByteCursor(destination, offset);

            cursor.WriteInt32(Version);
            cursor.WriteCompact((ulong)Inputs.Length);
            foreach (var input in Inputs)
            {
                cursor.Write(input.PreviousOutpoint.Hash);
                cursor.WriteUInt32(input.PreviousOutpoint.Index);
                cursor.WriteVarBytes(input.SignatureScript);
                cursor.WriteUInt32(input.Sequence);
            }
            cursor.WriteCompact((ulong)Outputs.Length);
            foreach (var output in Outputs)
            {
                cursor.WriteInt64(output.Amount);
                cursor.WriteVarBytes(output.PkScript);
            }
            cursor.WriteUInt32(LockTime);
        }
    }
}
