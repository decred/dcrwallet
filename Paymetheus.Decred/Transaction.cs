// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Util;
using System;
using System.Linq;

namespace Paymetheus.Decred
{
    public sealed class Transaction
    {
        public const int SupportedVersion = 1; // Version 1, full (prefix + witness) serialization

        public struct OutPoint
        {
            public const int SerializeSize = 41;

            public OutPoint(Blake256Hash hash, uint index, byte tree)
            {
                if (hash == null)
                    throw new ArgumentNullException(nameof(hash));

                Hash = hash;
                Index = index;
                Tree = tree;
            }

            public Blake256Hash Hash { get; }
            public uint Index { get; }
            public byte Tree { get; }

            public override string ToString() => $"{Hash}:{Index}";

            public bool IsNull() => Index == uint.MaxValue && Hash.Equals(Blake256Hash.Zero);
        }

        public struct Input
        {
            public Input(OutPoint previousOutPoint, uint sequence, Amount inputAmount,
                uint blockHeight, uint blockIndex, byte[] signatureScript)
            {
                if (signatureScript == null)
                    throw new ArgumentNullException(nameof(signatureScript));

                PreviousOutpoint = previousOutPoint;
                Sequence = sequence;

                SignatureScript = signatureScript;
                InputAmount = inputAmount;
                BlockHeight = blockHeight;
                BlockIndex = blockIndex;
            }

            public static Input CreateFromPrefix(OutPoint previousOutPoint, uint sequence) =>
                new Input(previousOutPoint, sequence, 0, 0, 0, new byte[0]);

            public static Input AddWitness(Input input, Amount inputAmount, uint blockHeight, uint blockIndex, byte[] signatureScript) =>
                new Input(input.PreviousOutpoint, input.Sequence, inputAmount, blockHeight, blockIndex, signatureScript);

            public OutPoint PreviousOutpoint { get; }
            public uint Sequence { get; }

            public Amount InputAmount { get; }
            public uint BlockHeight { get; }
            public uint BlockIndex { get; }
            public byte[] SignatureScript { get; }

            public int SerializeSize =>
                OutPoint.SerializeSize + 20 + CompactInt.SerializeSize((ulong)SignatureScript.Length) + SignatureScript.Length;
        }

        public struct Output
        {
            public const ushort LatestPkScriptVersion = 0;

            public Output(Amount amount, ushort version, byte[] pkScript)
            {
                if (pkScript == null)
                    throw new ArgumentNullException(nameof(pkScript));

                Amount = amount;
                Version = version;
                PkScript = pkScript;
            }

            public Amount Amount { get; }
            public ushort Version { get; }
            public byte[] PkScript { get; }

            public int SerializeSize =>
                10 + CompactInt.SerializeSize((ulong)PkScript.Length) + PkScript.Length;
        }

        public Transaction(int version, Input[] inputs, Output[] outputs, uint lockTime, uint expiry)
        {
            if (inputs == null)
                throw new ArgumentNullException(nameof(inputs));
            if (outputs == null)
                throw new ArgumentNullException(nameof(outputs));

            Version = version;
            Inputs = inputs;
            Outputs = outputs;
            LockTime = lockTime;
            Expiry = expiry;
        }

        public int Version { get; }
        public Input[] Inputs { get; }
        public Output[] Outputs { get; }
        public uint LockTime { get; }
        public uint Expiry { get; }

        public int SerializeSize
        {
            get
            {
                // Number of inputs is encoded twice.
                var size = 12 + (2 * CompactInt.SerializeSize((ulong)Inputs.Length)) +
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
            uint expiry;

            var cursor = new ByteCursor(rawTransaction);

            try
            {
                version = cursor.ReadInt32();
                if (version != SupportedVersion)
                {
                    var reason = $"Unsupported transaction version `{version}`";
                    throw new EncodingException(typeof(Transaction), cursor, reason);
                }

                // Prefix deserialization
                var prefixInputCount = cursor.ReadCompact();
                if (prefixInputCount > TransactionRules.MaxInputs)
                {
                    var reason = $"Input count {prefixInputCount} exceeds maximum value {TransactionRules.MaxInputs}";
                    throw new EncodingException(typeof(Transaction), cursor, reason);
                }
                inputs = new Input[prefixInputCount];
                for (int i = 0; i < (int)prefixInputCount; i++)
                {
                    var previousHash = new Blake256Hash(cursor.ReadBytes(Blake256Hash.Length));
                    var previousIndex = cursor.ReadUInt32();
                    var previousTree = cursor.ReadByte();
                    var previousOutPoint = new OutPoint(previousHash, previousIndex, previousTree);
                    var sequence = cursor.ReadUInt32();
                    inputs[i] = Input.CreateFromPrefix(previousOutPoint, sequence);
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
                    var outputVersion = cursor.ReadUInt16();
                    var pkScript = cursor.ReadVarBytes(TransactionRules.MaxPayload);
                    outputs[i] = new Output(amount, outputVersion, pkScript);
                }
                lockTime = cursor.ReadUInt32();
                expiry = cursor.ReadUInt32();

                // Witness deserialization
                var witnessInputCount = cursor.ReadCompact();
                if (witnessInputCount != prefixInputCount)
                {
                    var reason = $"Input counts in prefix and witness do not match ({prefixInputCount} != {witnessInputCount})";
                    throw new EncodingException(typeof(Transaction), cursor, reason);
                }
                for (int i = 0; i < (int)witnessInputCount; i++)
                {
                    var inputAmount = (Amount)cursor.ReadInt64();
                    var blockHeight = cursor.ReadUInt32();
                    var blockIndex = cursor.ReadUInt32();
                    var signatureScript = cursor.ReadVarBytes(TransactionRules.MaxPayload);
                    inputs[i] = Input.AddWitness(inputs[i], inputAmount, blockHeight, blockIndex, signatureScript);
                }
            }
            catch (Exception ex) when (!(ex is EncodingException))
            {
                throw new EncodingException(typeof(Transaction), cursor, ex);
            }

            return new Transaction(version, inputs, outputs, lockTime, expiry);
        }

        public byte[] Serialize()
        {
            var destination = new byte[SerializeSize];
            SerializeTo(destination);
            return destination;
        }

        public void SerializeTo(byte[] destination, int offset = 0)
        {
            if (destination == null)
                throw new ArgumentNullException(nameof(destination));

            var cursor = new ByteCursor(destination, offset);

            cursor.WriteInt32(Version);

            // Serialize prefix
            cursor.WriteCompact((ulong)Inputs.Length);
            foreach (var input in Inputs)
            {
                cursor.Write(input.PreviousOutpoint.Hash);
                cursor.WriteUInt32(input.PreviousOutpoint.Index);
                cursor.WriteByte(input.PreviousOutpoint.Tree);
                cursor.WriteUInt32(input.Sequence);
            }
            cursor.WriteCompact((ulong)Outputs.Length);
            foreach (var output in Outputs)
            {
                cursor.WriteInt64(output.Amount);
                cursor.WriteUInt16(output.Version);
                cursor.WriteVarBytes(output.PkScript);
            }
            cursor.WriteUInt32(LockTime);
            cursor.WriteUInt32(Expiry);

            // Serialize witness
            cursor.WriteCompact((ulong)Inputs.Length);
            foreach (var input in Inputs)
            {
                cursor.WriteInt64(input.InputAmount);
                cursor.WriteUInt32(input.BlockHeight);
                cursor.WriteUInt32(input.BlockIndex);
                cursor.WriteVarBytes(input.SignatureScript);
            }
        }

        public const int RedeemPayToPubKeyHashSigScriptSize = 1 + 73 + 1 + 33;
        public const int PayToPubKeyHashPkScriptSize = 1 + 1 + 1 + 20 + 1 + 1;

        // Worst case sizes for compressed p2pkh inputs and outputs.
        // Used for estimating an unsigned transaction's worst case serialize size
        // after it has been signed.
        public const int RedeemPayToPubKeyHashInputSize = 32 + 4 + 1 + 8 + 4 + 4 + 1 + RedeemPayToPubKeyHashSigScriptSize + 4;
        public const int PayToPubKeyHashOutputSize = 8 + 2 + 1 + PayToPubKeyHashPkScriptSize;

        /// <summary>
        /// Estimates the signed serialize size of an unsigned transaction, assuming all
        /// inputs spend P2PKH outputs.  The estimate is a worst case, so the actual
        /// serialize size after signing should always be less than or equal to this value.
        /// </summary>
        /// <param name="inputCount">Number of P2PKH outputs the transaction will redeem</param>
        /// <param name="outputs">Transaction output array</param>
        /// <param name="addChangeOutput">Add the serialize size for an additional P2PKH change output</param>
        /// <returns>Estimated signed serialize size of the transaction</returns>
        public static int EstimateSerializeSize(int inputCount, Output[] outputs, bool addChangeOutput)
        {
            if (outputs == null)
                throw new ArgumentNullException(nameof(outputs));

            var changeSize = 0;
            var outputCount = outputs.Length;
            if (addChangeOutput)
            {
                changeSize = PayToPubKeyHashOutputSize;
                outputCount++;
            }

            return
                12 + (2 * CompactInt.SerializeSize((ulong)inputCount)) + CompactInt.SerializeSize((ulong)outputCount) +
                inputCount * RedeemPayToPubKeyHashInputSize +
                outputs.Sum(o => o.SerializeSize) +
                changeSize;
        }
    }
}
